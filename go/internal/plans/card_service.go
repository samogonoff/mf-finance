package plans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
)

// CardService — процесс поверх карточек форм: создание карточек периода, переходы
// по маршруту со снапшотом версии, публикация при утверждении, reopen.
// Формо-специфичного здесь нет: значения для снапшота и строки для публикации
// отдают провайдеры (SnapshotProvider / PublishSource), зарегистрированные по
// form_code. Так одна оболочка обслуживает и МП, и розницу (ТЗ МП §2 / Розница §2).
type CardService struct {
	cards     CardStore
	publisher Publisher
	pubStore  PublishStore
	snapshots map[string]SnapshotProvider
	sources   map[string]PublishSource
	owners    StepOwnerResolver
	audit     AuditSink
}

// SnapshotProvider — отдаёт снимок значений формы для версии карточки.
// В снапшот входят значения + применённые условия/курсы (ТЗ МП §2.3).
type SnapshotProvider interface {
	FormSnapshot(ctx context.Context, c Card) (map[string]any, error)
}

// PublishSource — строки формы, готовые к публикации во внешний контур.
type PublishSource interface {
	PublishRows(ctx context.Context, c Card) ([]PublishRow, error)
}

// StepOwnerResolver — кто назначен на шаг маршрута (для skip_if_same_user).
type StepOwnerResolver interface {
	StepOwners(ctx context.Context, plID int64, formCode, scopeKey string) (map[string]int64, error)
}

// AuditSink — журнал модуля. Совпадает по сигнатуре с существующим Auditor
// (audit.go), чтобы карточки писали в тот же plans_audit_event.
type AuditSink interface {
	Record(ctx context.Context, e AuditEvent) error
	Enabled() bool
}

// NewCardService — конструктор.
func NewCardService(cards CardStore) *CardService {
	return &CardService{
		cards:     cards,
		snapshots: map[string]SnapshotProvider{},
		sources:   map[string]PublishSource{},
	}
}

// WithPublisher подключает публикацию (nil-безопасно: без него утверждение просто
// не публикует, статус остаётся approved).
func (s *CardService) WithPublisher(p Publisher, store PublishStore) *CardService {
	s.publisher, s.pubStore = p, store
	return s
}

// RegisterForm подключает провайдеры конкретной формы.
func (s *CardService) RegisterForm(formCode string, snap SnapshotProvider, src PublishSource) {
	if snap != nil {
		s.snapshots[formCode] = snap
	}
	if src != nil {
		s.sources[formCode] = src
	}
}

// WithOwners подключает резолвер владельцев шагов (skip_if_same_user).
func (s *CardService) WithOwners(r StepOwnerResolver) *CardService { s.owners = r; return s }

// WithAudit подключает журнал.
func (s *CardService) WithAudit(a AuditSink) *CardService { s.audit = a; return s }

// EnsureCards создаёт карточки периода по реестру форм (идемпотентно).
func (s *CardService) EnsureCards(ctx context.Context, plID int64, actorID int64) ([]Card, error) {
	for _, f := range FormRegistry() {
		for _, sc := range f.Scopes {
			if _, err := s.cards.EnsureCard(ctx, Card{
				PlID: plID, FormCode: f.Code, ScopeKey: sc.Key,
				Title: f.Name + " · " + sc.Title, Country: sc.Country,
				LegalEntity: sc.LegalEntity, Currency: sc.Currency, CreatedBy: actorID,
			}); err != nil {
				return nil, fmt.Errorf("карточка %s/%s: %w", f.Code, sc.Key, err)
			}
		}
	}
	return s.cards.Cards(ctx, plID)
}

// Cards — карточки периода.
func (s *CardService) Cards(ctx context.Context, plID int64) ([]Card, error) {
	return s.cards.Cards(ctx, plID)
}

// Card — одна карточка.
func (s *CardService) Card(ctx context.Context, id int64) (Card, error) {
	return s.cards.Card(ctx, id)
}

// Route — маршрут формы (все шаги, включая выключенные — для админки).
func (s *CardService) Route(ctx context.Context, formCode string) ([]RouteStep, error) {
	return s.cards.Route(ctx, formCode)
}

// SaveRouteStep — настройка шага маршрута (включение «Финансиста», сроки, роли).
func (s *CardService) SaveRouteStep(ctx context.Context, st RouteStep, actorID int64) error {
	if st.FormCode == "" || st.StepCode == "" {
		return errors.New("form_code и step_code обязательны")
	}
	if _, ok := formDef(st.FormCode); !ok {
		return fmt.Errorf("неизвестная форма %q", st.FormCode)
	}
	switch st.Kind {
	case StepFill, StepApprove, StepFinal:
	default:
		return fmt.Errorf("неизвестный вид шага %q", st.Kind)
	}
	if err := s.cards.UpsertRouteStep(ctx, st, actorID); err != nil {
		return err
	}
	s.rec(ctx, actorID, "route_step_save", "plans_form_route", 0, nil, st)
	return nil
}

// Action — переход карточки по маршруту. Возвращает новое состояние карточки.
// Публикация (если сработал шаг с publish_on_approve) выполняется после успешного
// сохранения перехода: провал публикации не откатывает утверждение, а переводит
// карточку в publish_failed с возможностью повтора (ТЗ Розница §2.2).
func (s *CardService) Action(ctx context.Context, cardID int64, in CardActionInput) (Card, error) {
	c, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return Card{}, err
	}
	route, err := s.cards.Route(ctx, c.FormCode)
	if err != nil {
		return Card{}, err
	}
	if in.StepOwner == nil && s.owners != nil {
		if owners, err := s.owners.StepOwners(ctx, c.PlID, c.FormCode, c.ScopeKey); err == nil {
			in.StepOwner = owners
		}
	}
	tr, err := applyCardAction(c, route, in)
	if err != nil {
		return Card{}, err
	}

	snapshot := []byte(`{}`)
	if tr.VersionBump {
		if p, ok := s.snapshots[c.FormCode]; ok {
			data, err := p.FormSnapshot(ctx, c)
			if err != nil {
				// Снимок — часть контракта версии, но недоступность источника факта
				// не должна блокировать процесс: пишем маркер и идём дальше.
				log.Printf("plans: снимок формы %s/%s не собран: %v", c.FormCode, c.ScopeKey, err)
				data = map[string]any{"error": err.Error()}
			}
			if raw, err := json.Marshal(data); err == nil {
				snapshot = raw
			}
		}
	}

	next, err := s.cards.SaveTransition(ctx, cardID, tr, snapshot, in.ActorID)
	if err != nil {
		return Card{}, err
	}
	s.rec(ctx, in.ActorID, "card_"+in.Action, "form_card", cardID,
		map[string]any{"status": c.Status, "step": c.StepCode},
		map[string]any{"status": next.Status, "step": next.StepCode, "comment": in.Comment})

	if tr.Publish {
		if pub, err := s.Publish(ctx, cardID, PublishModeWrite, in.ActorID); err != nil {
			log.Printf("plans: публикация карточки %d не удалась: %v", cardID, err)
		} else if pub.Status != "ok" {
			log.Printf("plans: публикация карточки %d: %s (%s)", cardID, pub.Status, pub.Error)
		}
		// Статус карточки уже обновлён внутри Publish; перечитываем.
		if fresh, err := s.cards.Card(ctx, cardID); err == nil {
			next = fresh
		}
	}
	return next, nil
}

// Approvals — лист согласования карточки (включая аннулированные решения).
func (s *CardService) Approvals(ctx context.Context, cardID int64) ([]CardApproval, error) {
	return s.cards.CardApprovals(ctx, cardID)
}

// Versions — история версий карточки.
func (s *CardService) Versions(ctx context.Context, cardID int64) ([]CardVersion, error) {
	return s.cards.CardVersions(ctx, cardID)
}

// VersionPayload — снимок конкретной версии (для diff «что изменилось»).
func (s *CardService) VersionPayload(ctx context.Context, cardID, versionNo int64) (json.RawMessage, error) {
	return s.cards.CardVersionPayload(ctx, cardID, int(versionNo))
}

// SetCalcMode — переключение направления расчёта карточки (legacy|inverse).
// Инверсия (ТЗ МП §3.1) включается на карточку, а не глобально: закрытые периоды
// продолжают считаться алгоритмом, которым были утверждены.
func (s *CardService) SetCalcMode(ctx context.Context, cardID int64, mode string, actorID int64) error {
	if mode != CalcLegacy && mode != CalcInverse {
		return fmt.Errorf("режим расчёта должен быть %q или %q", CalcLegacy, CalcInverse)
	}
	c, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return err
	}
	if c.Locked {
		return errors.New("период закрыт: режим расчёта не меняется")
	}
	if err := s.cards.SetCalcMode(ctx, cardID, mode); err != nil {
		return err
	}
	s.rec(ctx, actorID, "card_calc_mode", "form_card", cardID,
		map[string]any{"calc_mode": c.CalcMode}, map[string]any{"calc_mode": mode})
	return nil
}

// rec — событие в журнал модуля. Аудит выключен → no-op (как и раньше:
// PLANS_AUDIT_ENABLED). Детали before/after журнал не хранит, поэтому кладём
// только то, что у него есть: кто, что сделал и над каким объектом.
func (s *CardService) rec(ctx context.Context, userID int64, action, entity string, id int64, _, _ any) {
	if s.audit == nil || !s.audit.Enabled() {
		return
	}
	_ = s.audit.Record(ctx, AuditEvent{
		UserID: userID, Action: action, EntityType: entity, EntityID: id,
	})
}

package plans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
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
	// legalEntities — УНП ЮЛ по стране для колонки «Компания» приёмника тактики
	// (правки ТЗ 02.09.2026 §15/§16).
	legalEntities LegalEntityResolver
	// notify — уведомления по карточкам (кому форма пришла на решение). nil —
	// процесс работает молча, как раньше.
	notify CardNotifier
	// gates — проверки «можно ли отправить форму на согласование» по form_code.
	// Без гейта форма ведёт себя как раньше (валидации только в интерфейсе).
	gates map[string]SubmitGate
	// submitStrict — жёсткий режим гейта: замечания БЛОКИРУЮТ отправку.
	// Ответ 41 (10.09.2026): считать проверки всегда, а блокировку сделать
	// переключаемой; по умолчанию мягкий режим, жёсткий — к промышленной
	// эксплуатации. Поэтому false здесь — осознанный дефолт, а не забытый флаг.
	submitStrict bool
	// stages — этапы периода: их статус пересчитывается по карточкам, чтобы
	// таймлайн и формы не расходились (stage_sync.go).
	stages StageStore
	// fillTasks — задания заполнения: закрываются отправкой формы, возвращаются
	// вместе с ней (task_card_sync.go).
	fillTasks         FillTaskSyncer
	retailCountryGate bool
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
		cards:             cards,
		retailCountryGate: true,
		snapshots:         map[string]SnapshotProvider{},
		sources:           map[string]PublishSource{},
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

// WithLegalEntities подключает справочник ЮЛ: план пишется на реальный УНП.
func (s *CardService) WithLegalEntities(r LegalEntityResolver) *CardService {
	s.legalEntities = r
	return s
}

// WithOwners подключает резолвер владельцев шагов (skip_if_same_user).
func (s *CardService) WithOwners(r StepOwnerResolver) *CardService { s.owners = r; return s }

// WithAudit подключает журнал.
func (s *CardService) WithAudit(a AuditSink) *CardService { s.audit = a; return s }

// WithSubmitStrict включает жёсткий режим гейта отправки (ответ 41).
func (s *CardService) WithSubmitStrict(v bool) *CardService { s.submitStrict = v; return s }

// SubmitStrict — включён ли жёсткий режим. Нужен интерфейсу: в мягком режиме
// замечания показываются плашкой «не блокирует», а не «отправка заблокирована».
func (s *CardService) SubmitStrict() bool { return s.submitStrict }

// WithRetailCountryGate permits a test run through one country when explicitly disabled.
// Production defaults to the complete four-country route.
func (s *CardService) WithRetailCountryGate(v bool) *CardService { s.retailCountryGate = v; return s }

// EnsureCards создаёт карточки периода по реестру форм (идемпотентно).
func (s *CardService) EnsureCards(ctx context.Context, plID int64, actorID int64) ([]Card, error) {
	existing, err := s.cards.Cards(ctx, plID)
	if err != nil {
		return nil, err
	}
	for _, f := range FormRegistry() {
		closed := formClosedInPeriod(existing, f.Code)
		for _, sc := range f.Scopes {
			if sc.Late && closed && !hasCardScope(existing, f.Code, sc.Key) {
				continue
			}
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
	cards, err := s.cards.Cards(ctx, plID)
	if err != nil {
		return nil, err
	}
	for i := range cards {
		if s.retailCountryGate && cards[i].FormCode == TemplateRetail {
			cards[i].MissingCountries = missingRetailCountries(cards, cards[i].ID, false)
		}
	}
	return cards, nil
}

// Card — одна карточка.
func (s *CardService) Card(ctx context.Context, id int64) (Card, error) {
	c, err := s.cards.Card(ctx, id)
	if err != nil || !s.retailCountryGate || c.FormCode != TemplateRetail {
		return c, err
	}
	cards, err := s.cards.Cards(ctx, c.PlID)
	if err != nil {
		return Card{}, err
	}
	c.MissingCountries = missingRetailCountries(cards, c.ID, false)
	return c, nil
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
	in.Action = strings.TrimSpace(in.Action)
	c, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return Card{}, err
	}
	route, err := s.cards.Route(ctx, c.FormCode)
	if err != nil {
		return Card{}, err
	}
	// Отправка на согласование — единственное место, где формальные проверки
	// формы обязательны: дальше по маршруту значения уже заморожены.
	if strings.TrimSpace(in.Action) == "submit" {
		if c.FormCode == TemplateRetail && !in.RetailTotalsReviewed {
			return Card{}, errors.New("перед отправкой подтвердите итоги по четырём статусам LFL")
		}
		if err := s.checkSubmitGate(ctx, c); err != nil {
			return Card{}, err
		}
	}
	sales := c.FormCode == TemplateRetail || c.FormCode == TemplateMP
	if in.StepOwner == nil && s.owners != nil {
		owners, ownerErr := s.owners.StepOwners(ctx, c.PlID, c.FormCode, c.ScopeKey)
		if ownerErr != nil && sales {
			return Card{}, fmt.Errorf("не удалось определить ответственного карточки: %w", ownerErr)
		}
		in.StepOwner = owners
	}
	if sales && (in.Action == "submit" || in.Action == "approve" || in.Action == "return") && in.StepOwner == nil {
		return Card{}, errors.New("ответственные за маршрут продаж не настроены")
	}
	tr, err := applyCardAction(c, route, in)
	if err != nil {
		return Card{}, err
	}
	if sales && (in.Action == "submit" || in.Action == "approve" || in.Action == "return") {
		if in.StepOwner[c.StepCode] == 0 && in.Action != "submit" {
			return Card{}, fmt.Errorf("не назначен ответственный текущего шага %s карточки %s/%s", c.StepCode, c.FormCode, c.ScopeKey)
		}
		if (tr.Status == CardOnApproval || tr.Status == CardReturned) && in.StepOwner[tr.StepCode] == 0 {
			return Card{}, fmt.Errorf("не назначен ответственный следующего шага %s карточки %s/%s", tr.StepCode, c.FormCode, c.ScopeKey)
		}
	}
	// Директор принимает свою страну независимо от остальных. Условие «все четыре страны» относится к
	// решению шага 1.4: утвердить или вернуть розницу Сипарова может, когда страны собрались.
	if s.retailCountryGate && retailFinalDecision(c, in.Action) {
		cards, err := s.cards.Cards(ctx, c.PlID)
		if err != nil {
			return Card{}, err
		}
		if missing := missingRetailCountries(cards, c.ID, true); len(missing) > 0 {
			return Card{}, errors.New(retailFinalWaitError(missing))
		}
	}
	tr.ExpectedStatus, tr.ExpectedStep, tr.ExpectedVersion = c.Status, c.StepCode, c.CurrentVersion
	tr.ExpectedUpdatedAt = c.UpdatedAt
	if c.FormCode == TemplateRetail {
		tr.RetailPlID = c.PlID
		tr.RequireRetailCountryGate = s.retailCountryGate && retailFinalDecision(c, in.Action)
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
	// Этапы периода — агрегат состояния карточек: пересчитываем сразу после
	// перехода, иначе таймлайн показывает не то, что происходит с формами.
	s.syncStages(ctx, next.PlID)
	// Уведомления — последним шагом и в фоне: получатель узнаёт об уже
	// состоявшемся переходе, а сбой канала не откатывает маршрут.
	s.notifyCardTransition(ctx, c, next, in)
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

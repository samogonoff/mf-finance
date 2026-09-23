package bugtracker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/company/finance-api/internal/notifications"
)

const dedupWindow = 24 * time.Hour

// defaultAgent — кем подписывается автоматизация, если не передала своё имя.
const defaultAgent = "hermes"

// Ошибки сервиса — handler переводит их в HTTP-коды.
var (
	ErrNotFound = errors.New("репорт не найден")
	// ErrSourceNotFound — передан source_id, которого нет в справочнике.
	ErrSourceNotFound = errors.New("источник проблемы не найден")
)

// ValidationError — 400 с человекочитаемым текстом.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// ConflictError — 409: репорт уже взят в работу или не в статусе «Новый».
type ConflictError struct {
	Status    string
	ClaimedBy *string
}

func (e *ConflictError) Error() string {
	return "репорт уже взят в работу или не в статусе «Новый»"
}

var typeLabels = map[string]string{
	TypeBug: "Баг", TypeData: "Данные", TypeUI: "UI",
	TypePerformance: "Производительность", TypeOther: "Другое",
}

type Service struct {
	repo    *Repo
	storage *Storage
	notif   *notifications.Service
}

func NewService(repo *Repo, storage *Storage, notif *notifications.Service) *Service {
	return &Service{repo: repo, storage: storage, notif: notif}
}

// reportURL — адрес страницы: отдельное поле, а у старого фронта — route.url.
func reportURL(in CreateInput) string {
	if in.URL != "" {
		return in.URL
	}
	u, _ := in.Route["url"].(string)
	return u
}

// computeSignature — SHA256(section|url|title|description).
func computeSignature(in CreateInput) string {
	h := sha256.New()
	h.Write([]byte(strings.ToLower(in.Section)))
	h.Write([]byte("|"))
	h.Write([]byte(strings.ToLower(reportURL(in))))
	h.Write([]byte("|"))
	h.Write([]byte(strings.ToLower(strings.TrimSpace(in.Title))))
	h.Write([]byte("|"))
	h.Write([]byte(strings.ToLower(strings.TrimSpace(in.Description))))
	return hex.EncodeToString(h.Sum(nil))
}

// normalizeCreate — белые списки и пределы из MP: неизвестный раздел → other,
// тексты обрезаются, из логов остаются последние maxLogEntries записей.
func normalizeCreate(in CreateInput) (CreateInput, error) {
	if !IsValidSection(in.Section) {
		in.Section = "other"
	}
	if !IsValidType(in.Type) {
		in.Type = TypeBug
	}
	in.Title = truncateRunes(strings.TrimSpace(in.Title), maxTitleRunes)
	if in.Title == "" {
		return in, &ValidationError{"title required"}
	}
	in.Description = truncateRunes(strings.TrimSpace(in.Description), maxDescriptionRunes)
	in.Steps = truncateRunes(strings.TrimSpace(in.Steps), maxStepsRunes)
	in.URL = truncateRunes(strings.TrimSpace(reportURL(in)), maxURLRunes)
	in.ConsoleLogs = capTail(in.ConsoleLogs, maxLogEntries)
	in.NetworkErrors = capTail(in.NetworkErrors, maxLogEntries)
	in.JSErrors = capTail(in.JSErrors, maxLogEntries)
	return in, nil
}

// Create — основной поток создания репорта.
// Возвращает либо новый Report, либо найденный недавний дубль (deduplicated=true).
func (s *Service) Create(
	ctx context.Context, userID *int64, in CreateInput, files []*multipart.FileHeader,
) (rep *Report, deduplicated bool, err error) {
	in, err = normalizeCreate(in)
	if err != nil {
		return nil, false, err
	}

	sig := computeSignature(in)

	if dupID, err := s.repo.FindRecentDuplicate(ctx, userID, sig, dedupWindow); err != nil {
		return nil, false, err
	} else if dupID != 0 {
		rep, err = s.repo.Get(ctx, dupID)
		return rep, true, err
	}

	id, err := s.repo.Create(ctx, userID, in, sig)
	if err != nil {
		return nil, false, err
	}

	if len(files) > 0 {
		names, err := s.storage.SaveAll(id, files)
		if err != nil {
			// откатываем — пустой репорт уже не полезен.
			_ = s.repo.Delete(ctx, id)
			return nil, false, err
		}
		if err := s.repo.UpdateScreenshots(ctx, id, names); err != nil {
			return nil, false, err
		}
	}

	rep, err = s.repo.Get(ctx, id)
	if err != nil {
		return nil, false, err
	}

	s.notifyAdmins(newReportNotification(rep))
	return rep, false, nil
}

// newReportNotification — текст как в MP: номер, тип, раздел, автор, страница.
func newReportNotification(rep *Report) notifications.Input {
	author := "неизвестен"
	var authorID any
	if rep.User != nil {
		authorID = rep.User.ID
		author = rep.User.Email
		if rep.User.Name != "" {
			author = rep.User.Name
		}
	}
	msg := fmt.Sprintf("Раздел: %s\nАвтор: %s", rep.Section, author)
	if rep.URL != "" {
		msg += "\nURL: " + rep.URL
	}
	msg += "\n\n" + rep.Title
	if rep.Description != "" {
		msg += "\n" + truncateRunes(rep.Description, 240)
	}
	return notifications.Input{
		Title:      fmt.Sprintf("Новая заявка #%d из багтрекера (%s)", rep.ID, typeLabel(rep.Type)),
		Message:    msg,
		Type:       notifications.TypeWarning,
		ObjectType: notifications.ObjBugReportNew,
		Data: map[string]any{
			"report_id": rep.ID,
			"section":   rep.Section,
			"type":      rep.Type,
			"author_id": authorID,
			"url":       fmt.Sprintf("/admin/bugtracker?id=%d", rep.ID),
		},
	}
}

// notifyAdmins — best-effort: сбой уведомления не валит основную операцию.
func (s *Service) notifyAdmins(in notifications.Input) {
	if s.notif == nil {
		return
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if _, err := s.notif.CreateForAllAdmins(bgCtx, in); err != nil {
			log.Printf("bugtracker: уведомление админам не отправлено: %v", err)
		}
	}()
}

// Get — репорт по id; ErrNotFound, если его нет.
func (s *Service) Get(ctx context.Context, id int64) (*Report, error) {
	rep, err := s.repo.Get(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return rep, err
}

// validatePatch — «Решён» без причины не даёт копить статистику по источникам,
// поэтому причина обязательна: в самом запросе или уже проставленная (например,
// автоматикой вместе с MR).
func validatePatch(cur *Report, p PatchInput) error {
	if p.Status != nil && !IsValidStatus(*p.Status) {
		return &ValidationError{"invalid status"}
	}
	status := cur.Status
	if p.Status != nil {
		status = *p.Status
	}
	if status != StatusResolved {
		return nil
	}
	hasSource := cur.SourceID != nil
	if p.SourceID != nil {
		hasSource = *p.SourceID > 0
	}
	if !hasSource {
		return &ValidationError{"для статуса «Решён» нужен источник проблемы (source_id)"}
	}
	return nil
}

// Patch — правка из админки.
func (s *Service) Patch(ctx context.Context, id int64, p PatchInput) (*Report, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := validatePatch(cur, p); err != nil {
		return nil, err
	}
	if p.SourceID != nil && *p.SourceID > 0 {
		if err := s.requireSource(ctx, *p.SourceID); err != nil {
			return nil, err
		}
	}
	if err := s.repo.Patch(ctx, id, p); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id)
}

func (s *Service) requireSource(ctx context.Context, id int64) error {
	ok, err := s.repo.SourceExists(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSourceNotFound
	}
	return nil
}

// ── Автоматический разбор ───────────────────────────────────────────────────
//
// Цикл: queue (что чинить) → claim (замок, чтобы не разобрали дважды) →
// mr (готов merge request, админам летит уведомление) → resolve (после выката)
// либо release (агент не справился, баг возвращается в очередь людям).

func agentName(by string) string {
	by = strings.TrimSpace(by)
	if by == "" {
		return defaultAgent
	}
	return truncateRunes(by, maxAgentRunes)
}

// Queue — новые, никем не взятые репорты; sections фильтруются по белому списку.
func (s *Service) Queue(ctx context.Context, limit int, sections []string) ([]Report, error) {
	valid := []string{}
	for _, sec := range sections {
		if sec = strings.TrimSpace(sec); IsValidSection(sec) {
			valid = append(valid, sec)
		}
	}
	return s.repo.ListUnclaimedNew(ctx, limit, valid)
}

// Claim — new → in_progress под замком. ConflictError, если уже взят или не new.
func (s *Service) Claim(ctx context.Context, id int64, by string) (*Report, error) {
	by = agentName(by)
	ok, err := s.repo.Claim(ctx, id, by)
	if err != nil {
		return nil, err
	}
	rep, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &ConflictError{Status: rep.Status, ClaimedBy: rep.ClaimedBy}
	}
	log.Printf("bugtracker: баг #%d взят в работу автоматизацией (%s)", id, by)
	return rep, nil
}

// AttachMR — привязать готовый MR и позвать админов выкатывать.
func (s *Service) AttachMR(ctx context.Context, id int64, in MRInput) (*Report, error) {
	in.MRURL = strings.TrimSpace(in.MRURL)
	in.Summary = strings.TrimSpace(in.Summary)
	if in.MRIID <= 0 || in.MRURL == "" {
		return nil, &ValidationError{"нужны mr_iid и mr_url"}
	}
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	if in.SourceID > 0 {
		if err := s.requireSource(ctx, in.SourceID); err != nil {
			return nil, err
		}
	}
	if err := s.repo.AttachMR(ctx, id, in); err != nil {
		return nil, err
	}
	rep, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	s.notifyAdmins(notifications.Input{
		Title:      fmt.Sprintf("Готов MR по багу #%d", id),
		Message:    mrNotificationText(rep, in.Summary),
		Type:       notifications.TypeInfo,
		ObjectType: notifications.ObjBugReportMR,
		Data: map[string]any{
			"report_id": rep.ID,
			"mr_iid":    in.MRIID,
			"mr_url":    in.MRURL,
			"url":       fmt.Sprintf("/admin/bugtracker?id=%d", rep.ID),
		},
	})
	log.Printf("bugtracker: к багу #%d привязан MR !%d", id, in.MRIID)
	return rep, nil
}

// mrNotificationText — команду выката сюда не пишем: уведомление дублируется
// в Б24, где бота, который её понимает, нет. Номер берут из ссылки на MR.
func mrNotificationText(rep *Report, summary string) string {
	lines := []string{
		fmt.Sprintf("Раздел: %s, тип: %s", rep.Section, typeLabel(rep.Type)),
		"Жалоба: " + truncateRunes(rep.Title, 300),
	}
	if summary != "" {
		lines = append(lines, "Что сделано: "+truncateRunes(summary, 500))
	}
	if rep.MRURL != nil {
		lines = append(lines, "MR: "+*rep.MRURL)
	}
	return strings.Join(lines, "\n")
}

// Release — вернуть баг в очередь. Агент обязан звать это, когда не справился,
// иначе баг навсегда останется in_progress и выпадет из поля зрения людей.
func (s *Service) Release(ctx context.Context, id int64, reason string) (*Report, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return nil, err
	}
	comment := ""
	if reason = strings.TrimSpace(reason); reason != "" {
		comment = "Автоматика не справилась: " + reason
	}
	if err := s.repo.Release(ctx, id, comment); err != nil {
		return nil, err
	}
	log.Printf("bugtracker: баг #%d возвращён в очередь", id)
	return s.repo.Get(ctx, id)
}

// Resolve — закрыть баг после выката. Причину можно не передавать, если она
// уже проставлена вместе с MR.
func (s *Service) Resolve(ctx context.Context, id int64, in ResolveInput) (*Report, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.SourceID > 0 {
		if err := s.requireSource(ctx, in.SourceID); err != nil {
			return nil, err
		}
	} else if cur.SourceID == nil {
		return nil, &ValidationError{"нужен source_id: причина не была указана при привязке MR"}
	}
	if err := s.repo.Resolve(ctx, id, in.SourceID, strings.TrimSpace(in.Comment)); err != nil {
		return nil, err
	}
	log.Printf("bugtracker: баг #%d закрыт после выката", id)
	return s.repo.Get(ctx, id)
}

func (s *Service) PendingMR(ctx context.Context, limit int) ([]Report, error) {
	return s.repo.ListPendingMR(ctx, limit)
}

func (s *Service) ByMR(ctx context.Context, iid int) ([]Report, error) {
	return s.repo.ListByMR(ctx, iid)
}

// normalizeAuto — находка мониторинга: раздел по умолчанию ops, заголовок —
// первая строка описания, сигнатура — своя у автоматики или вычисленная.
func normalizeAuto(in AutoCreateInput) (AutoCreateInput, error) {
	in.Description = truncateRunes(strings.TrimSpace(in.Description), maxDescriptionRunes)
	if in.Description == "" {
		return in, &ValidationError{"нужно description"}
	}
	if in.Section == "" {
		in.Section = SectionOps
	}
	if !IsValidSection(in.Section) {
		return in, &ValidationError{"неизвестный section"}
	}
	if in.Type == "" {
		in.Type = TypeBug
	}
	if !IsValidType(in.Type) {
		return in, &ValidationError{"неизвестный type"}
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		in.Title, _, _ = strings.Cut(in.Description, "\n")
	}
	in.Title = truncateRunes(strings.TrimSpace(in.Title), maxTitleRunes)
	in.Steps = truncateRunes(strings.TrimSpace(in.Steps), maxStepsRunes)
	in.URL = truncateRunes(strings.TrimSpace(in.URL), maxURLRunes)
	in.Signature = truncateRunes(strings.TrimSpace(in.Signature), 200)
	if in.Signature == "" {
		in.Signature = computeSignature(CreateInput{
			Section: in.Section, Title: in.Title, Description: in.Description, URL: in.URL,
		})
	}
	in.By = agentName(in.By)
	return in, nil
}

// CreateAuto — завести баг от автоматики. Повторная находка той же незакрытой
// проблемы (проверка гоняется ежедневно) возвращает существующий баг.
func (s *Service) CreateAuto(ctx context.Context, in AutoCreateInput) (rep *Report, duplicate bool, err error) {
	in, err = normalizeAuto(in)
	if err != nil {
		return nil, false, err
	}
	if dupID, err := s.repo.FindOpenBySignature(ctx, in.Signature); err != nil {
		return nil, false, err
	} else if dupID != 0 {
		rep, err = s.repo.Get(ctx, dupID)
		return rep, true, err
	}
	id, err := s.repo.CreateAuto(ctx, in, in.Signature, "Заведено автоматикой: "+in.By)
	if err != nil {
		return nil, false, err
	}
	log.Printf("bugtracker: автоматика (%s) завела баг #%d в разделе %s", in.By, id, in.Section)
	rep, err = s.repo.Get(ctx, id)
	return rep, false, err
}

// ── helpers ─────────────────────────────────────────────────────────────────

func typeLabel(t string) string {
	if l, ok := typeLabels[t]; ok {
		return l
	}
	return t
}

// truncateRunes — обрезка по символам, а не байтам: кириллица иначе режется
// посреди руны и в уведомление уходит битый UTF-8.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// capTail — последние n элементов (свежие события важнее старых).
func capTail(a []any, n int) []any {
	if len(a) <= n {
		return a
	}
	return a[len(a)-n:]
}

package plans

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// Публикация утверждённого плана во внешний контур (ТЗ МП §9.3, Розница §7.2).
//
// Приёмники — семейство Budgeting.dbo.FormToLoa*/VFORMTOLOAD*: девять колонок,
// БЕЗ первичного ключа и без колонок сценария/ЮЛ. Отсюда три следствия:
//  1. сценарий задаётся ВЫБОРОМ ТАБЛИЦЫ (тактика → FormToLoaTaktTarget / VFORMTOLOADTAKTTARGET);
//  2. логическая уникальность строки — Параметр+Страна+КодЦФО+КодPL+Дата, и без
//     уникального индекса повторная публикация даёт ДУБЛИ, а не обновление;
//  3. идемпотентность обеспечивает сервис: DELETE по логическому ключу + INSERT
//     в одной транзакции, затем контроль «Σ введено − Σ прочитано из приёмника = 0».
//
// Открытые вопросы BI (§12: какой «Параметр», агрегат vs детализация, кто пишет
// BYN-пару) живут в таблице publish_mapping — это данные, а не код. Пока строки
// маппинга выключены, публикация работает ТОЛЬКО в режиме dry-run: считает, что
// ушло бы, сверяет с приёмником и отдаёт отчёт. Этим же отчётом BI подтверждает
// решения, после чего строки включаются и режим write разрешается.

// Режимы публикации.
const (
	PublishModeDryRun = "dry_run"
	PublishModeWrite  = "write"
)

// PublishRow — одна строка формы, готовая к отправке в приёмник.
type PublishRow struct {
	BlockType   string    `json:"block_type"`
	Param       string    `json:"param"`
	Country     string    `json:"country"`
	CodeCFO     int       `json:"code_cfo"`
	GroupCFO1   string    `json:"group_cfo1"`
	GroupCFO2   string    `json:"group_cfo2"`
	CFOName     string    `json:"cfo"`
	CodePL      int       `json:"code_pl"`
	Date        time.Time `json:"date"`
	Value       float64   `json:"value"`
	Currency    string    `json:"currency"`
	TargetTable string    `json:"target_table"`
}

// PublishMapping — правило маппинга строки формы в приёмник (publish_mapping).
type PublishMapping struct {
	ID             int64  `json:"id"`
	FormCode       string `json:"form_code"`
	BlockType      string `json:"block_type"`
	Country        string `json:"country"`
	ParamName      string `json:"param_name"`
	CodePL         int    `json:"code_pl"`
	TargetTable    string `json:"target_table"`
	TargetCurrency string `json:"target_currency"`
	Aggregate      bool   `json:"aggregate"`
	AggregateCFO   int    `json:"aggregate_cfo"`
	Enabled        bool   `json:"enabled"`
	Note           string `json:"note"`
}

// PublishResult — итог публикации (и dry-run, и записи).
type PublishResult struct {
	CardID      int64            `json:"card_id"`
	VersionNo   int              `json:"version_no"`
	Mode        string           `json:"mode"`
	Status      string           `json:"status"` // ok|failed|blocked
	Target      string           `json:"target"`
	RowsTotal   int              `json:"rows_total"`
	RowsWritten int              `json:"rows_written"`
	SumInput    float64          `json:"sum_input"`
	SumTarget   float64          `json:"sum_target"`
	Diff        float64          `json:"diff"`
	Error       string           `json:"error,omitempty"`
	Unmapped    []string         `json:"unmapped,omitempty"` // строки формы без правила маппинга
	Pending     []string         `json:"pending,omitempty"`  // правила есть, но выключены (ждут BI)
	Preview     []PublishRow     `json:"preview,omitempty"`  // первые строки — для отчёта dry-run
	Log         []PublishLogItem `json:"log,omitempty"`
}

// PublishLogItem — запись журнала публикаций.
type PublishLogItem struct {
	ID        int64     `json:"id"`
	VersionNo int       `json:"version_no"`
	Mode      string    `json:"mode"`
	Target    string    `json:"target"`
	Status    string    `json:"status"`
	RowsTotal int       `json:"rows_total"`
	RowsOK    int       `json:"rows_written"`
	Diff      float64   `json:"diff"`
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// Publisher — запись строк в приёмник. Реализации: mssqlPublisher (прод) и
// nopPublisher (dev/без MSSQL — только dry-run).
type Publisher interface {
	// Write — идемпотентная запись пачки строк в одну таблицу-приёмник.
	// Возвращает число записанных строк и сумму значений, прочитанную ИЗ приёмника
	// после записи (для контроля «введено = записано»).
	Write(ctx context.Context, target string, rows []PublishRow) (written int, sumTarget float64, err error)
	// Probe — сумма значений в приёмнике по логическому ключу пачки (без записи).
	Probe(ctx context.Context, target string, rows []PublishRow) (sumTarget float64, err error)
	// Available — есть ли соединение с приёмником.
	Available() bool
}

// PublishStore — маппинг и журнал (миграция 0032).
type PublishStore interface {
	Mappings(ctx context.Context, formCode string) ([]PublishMapping, error)
	UpsertMapping(ctx context.Context, m PublishMapping, actorID int64) error
	LogPublish(ctx context.Context, r PublishResult, actorID int64, report []byte) error
	PublishLog(ctx context.Context, cardID int64) ([]PublishLogItem, error)
	AlreadyPublished(ctx context.Context, key string) (bool, error)
}

// Publish — собрать строки формы, применить маппинг и (в режиме write) записать.
// Идемпотентность: ключ card:version:target; повторный запуск с тем же ключом не
// пишет второй раз.
func (s *CardService) Publish(ctx context.Context, cardID int64, mode string, actorID int64) (PublishResult, error) {
	c, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return PublishResult{}, err
	}
	res := PublishResult{CardID: cardID, VersionNo: c.CurrentVersion, Mode: mode, Status: "ok"}

	src, ok := s.sources[c.FormCode]
	if !ok {
		res.Status = "blocked"
		res.Error = "для формы " + c.FormCode + " не подключён источник строк публикации"
		return res, nil
	}
	if s.pubStore == nil {
		res.Status = "blocked"
		res.Error = "маппинг публикации недоступен"
		return res, nil
	}
	rows, err := src.PublishRows(ctx, c)
	if err != nil {
		res.Status = "failed"
		res.Error = err.Error()
		s.logPublish(ctx, res, actorID)
		return res, err
	}
	mappings, err := s.pubStore.Mappings(ctx, c.FormCode)
	if err != nil {
		return res, err
	}

	mapped, rep := applyPublishMapping(c, rows, mappings)
	res.RowsTotal = len(mapped)
	res.Unmapped, res.Pending = rep.unmapped, rep.pending
	for _, r := range mapped {
		res.SumInput += r.Value
	}
	if len(mapped) > 0 {
		res.Target = mapped[0].TargetTable
		if len(mapped) > 20 {
			res.Preview = mapped[:20]
		} else {
			res.Preview = mapped
		}
	}

	// Публикация невозможна, пока маппинг не подтверждён BI: остаётся dry-run.
	if len(mapped) == 0 {
		res.Status = "blocked"
		if len(rep.pending) > 0 {
			res.Error = "маппинг публикации ещё не подтверждён BI (правила выключены): " +
				strings.Join(rep.pending, "; ")
		} else {
			res.Error = "нет строк для публикации"
		}
		s.logPublish(ctx, res, actorID)
		return res, nil
	}

	if s.publisher == nil || !s.publisher.Available() {
		res.Mode = PublishModeDryRun
		res.Status = "blocked"
		res.Error = "приёмник недоступен (нет соединения с Budgeting) — доступен только предпросмотр"
		s.logPublish(ctx, res, actorID)
		return res, nil
	}

	if mode != PublishModeWrite {
		res.Mode = PublishModeDryRun
		sum, err := s.publisher.Probe(ctx, res.Target, mapped)
		if err != nil {
			res.Status = "failed"
			res.Error = err.Error()
		} else {
			res.SumTarget = sum
			res.Diff = round4(res.SumInput - sum)
		}
		s.logPublish(ctx, res, actorID)
		return res, nil
	}

	// Идемпотентность: одна успешная запись на (карточка, версия, приёмник).
	key := fmt.Sprintf("card:%d:v%d:%s", cardID, c.CurrentVersion, res.Target)
	if done, err := s.pubStore.AlreadyPublished(ctx, key); err == nil && done {
		res.Status = "ok"
		res.Error = "версия уже опубликована — повтор не выполняется"
		return res, nil
	}
	written, sumTarget, err := s.publisher.Write(ctx, res.Target, mapped)
	res.RowsWritten, res.SumTarget = written, sumTarget
	res.Diff = round4(res.SumInput - sumTarget)
	if err != nil {
		res.Status = "failed"
		res.Error = err.Error()
	} else if math.Abs(res.Diff) > 0.01 {
		// Контроль записи (ТЗ МП §9.3): «Σ введённое × курс − Σ прочитанное = 0».
		res.Status = "failed"
		res.Error = fmt.Sprintf("контроль записи не сошёлся: введено %.2f, в приёмнике %.2f (расхождение %.2f)",
			res.SumInput, res.SumTarget, res.Diff)
	}
	s.logPublishKey(ctx, res, actorID, key)
	if err2 := s.cards.MarkPublished(ctx, cardID, res.Status == "ok", res.Error); err2 != nil {
		return res, err2
	}
	return res, err
}

// PublishLog — журнал публикаций карточки.
func (s *CardService) PublishLog(ctx context.Context, cardID int64) ([]PublishLogItem, error) {
	if s.pubStore == nil {
		return nil, errors.New("журнал публикаций недоступен")
	}
	return s.pubStore.PublishLog(ctx, cardID)
}

// Mappings — правила маппинга формы (для админки/отчёта dry-run).
func (s *CardService) Mappings(ctx context.Context, formCode string) ([]PublishMapping, error) {
	if s.pubStore == nil {
		return nil, errors.New("маппинг публикации недоступен")
	}
	return s.pubStore.Mappings(ctx, formCode)
}

// SaveMapping — правка правила маппинга (ответы BI ложатся данными).
func (s *CardService) SaveMapping(ctx context.Context, m PublishMapping, actorID int64) error {
	if s.pubStore == nil {
		return errors.New("маппинг публикации недоступен")
	}
	if m.FormCode == "" || m.ParamName == "" || m.TargetTable == "" {
		return errors.New("form_code, param_name и target_table обязательны")
	}
	if err := s.pubStore.UpsertMapping(ctx, m, actorID); err != nil {
		return err
	}
	s.rec(ctx, actorID, "publish_mapping_save", "publish_mapping", m.ID, nil, m)
	return nil
}

func (s *CardService) logPublish(ctx context.Context, res PublishResult, actorID int64) {
	s.logPublishKey(ctx, res, actorID, "")
}

func (s *CardService) logPublishKey(ctx context.Context, res PublishResult, actorID int64, key string) {
	if s.pubStore == nil {
		return
	}
	report, _ := json.Marshal(map[string]any{
		"unmapped": res.Unmapped,
		"pending":  res.Pending,
		"preview":  res.Preview,
		"key":      key,
	})
	if err := s.pubStore.LogPublish(ctx, res, actorID, report); err != nil {
		// Журнал не должен ломать процесс, но молчать о потере тоже нельзя.
		fmt.Printf("plans: журнал публикации не записан: %v\n", err)
	}
}

// mappingReport — что осталось за пределами маппинга.
type mappingReport struct {
	unmapped []string
	pending  []string
}

// applyPublishMapping применяет правила к строкам формы. Правила ищутся по
// (form_code, block_type, country) с фолбэком на пустые значения — от частного к
// общему. Выключенные правила не публикуются, но попадают в pending: это и есть
// перечень ожидающих ответов BI.
func applyPublishMapping(c Card, rows []PublishRow, mappings []PublishMapping) ([]PublishRow, mappingReport) {
	var out []PublishRow
	rep := mappingReport{}
	seenUnmapped := map[string]bool{}
	seenPending := map[string]bool{}

	for _, r := range rows {
		m, ok := pickMapping(mappings, r.BlockType, r.Country)
		if !ok {
			if !seenUnmapped[r.BlockType] {
				seenUnmapped[r.BlockType] = true
				rep.unmapped = append(rep.unmapped, r.BlockType)
			}
			continue
		}
		if !m.Enabled {
			label := m.BlockType + " → " + m.ParamName
			if m.Note != "" {
				label += " (" + m.Note + ")"
			}
			if !seenPending[label] {
				seenPending[label] = true
				rep.pending = append(rep.pending, label)
			}
			continue
		}
		row := r
		row.Param = m.ParamName
		row.TargetTable = m.TargetTable
		if m.CodePL > 0 {
			row.CodePL = m.CodePL
		}
		if m.Aggregate && m.AggregateCFO > 0 {
			row.CodeCFO = m.AggregateCFO
		}
		if row.Country == "" {
			row.Country = c.Country
		}
		out = append(out, row)
	}

	// Агрегирующие правила требуют свёртки по логическому ключу.
	out = foldPublishRows(out)
	return out, rep
}

// pickMapping — правило от частного (block+country) к общему (block, затем всё).
func pickMapping(mappings []PublishMapping, block, country string) (PublishMapping, bool) {
	var byBlock, generic PublishMapping
	var okBlock, okGeneric bool
	for _, m := range mappings {
		switch {
		case m.BlockType == block && m.Country == country && country != "":
			return m, true
		case m.BlockType == block && m.Country == "":
			byBlock, okBlock = m, true
		case m.BlockType == "" && m.Country == "":
			generic, okGeneric = m, true
		}
	}
	if okBlock {
		return byBlock, true
	}
	if okGeneric {
		return generic, true
	}
	return PublishMapping{}, false
}

// foldPublishRows — свёртка по логическому ключу приёмника
// (Параметр+Страна+КодЦФО+КодPL+Дата): после агрегации по группе (250/480)
// строки нескольких площадок обязаны сложиться в одну, иначе приёмник получит
// дубли ключа.
func foldPublishRows(rows []PublishRow) []PublishRow {
	if len(rows) < 2 {
		return rows
	}
	type key struct {
		param, country, target string
		cfo, pl                int
		date                   string
	}
	idx := map[key]int{}
	var out []PublishRow
	for _, r := range rows {
		k := key{r.Param, r.Country, r.TargetTable, r.CodeCFO, r.CodePL, r.Date.Format("2006-01-02")}
		if i, ok := idx[k]; ok {
			out[i].Value = round4(out[i].Value + r.Value)
			continue
		}
		idx[k] = len(out)
		out = append(out, r)
	}
	return out
}

func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

// ---------- Реализации Publisher ----------

// nopPublisher — заглушка, когда MSSQL недоступен (dev / PLANS_MOCK).
type nopPublisher struct{}

// NewNopPublisher — публикация недоступна: сервис остаётся в dry-run.
func NewNopPublisher() Publisher { return nopPublisher{} }

func (nopPublisher) Write(context.Context, string, []PublishRow) (int, float64, error) {
	return 0, 0, errors.New("публикация недоступна: нет соединения с Budgeting")
}
func (nopPublisher) Probe(context.Context, string, []PublishRow) (float64, error) {
	return 0, errors.New("приёмник недоступен")
}
func (nopPublisher) Available() bool { return false }

// mssqlPublisher — запись в приёмники Budgeting через тот же *sql.DB, что читает
// факт (MSSQL_PREMASTER_*). Транзакция на пачку: DELETE по логическому ключу +
// INSERT. Первичного ключа в приёмниках нет, поэтому идемпотентность делаем сами.
type mssqlPublisher struct {
	db      *sql.DB
	allowed map[string]bool // белый список таблиц-приёмников
}

// NewMssqlPublisher — конструктор. targets — разрешённые таблицы (из конфига),
// чтобы имя таблицы из данных маппинга не превращалось в произвольный SQL.
func NewMssqlPublisher(db *sql.DB, targets []string) Publisher {
	allowed := map[string]bool{}
	for _, t := range targets {
		if t = strings.TrimSpace(t); t != "" {
			allowed[strings.ToLower(t)] = true
		}
	}
	return &mssqlPublisher{db: db, allowed: allowed}
}

func (p *mssqlPublisher) Available() bool { return p != nil && p.db != nil }

// checkTarget — защита от инъекции через данные маппинга: пишем только в таблицы
// из белого списка конфига.
func (p *mssqlPublisher) checkTarget(target string) error {
	if !p.allowed[strings.ToLower(strings.TrimSpace(target))] {
		return fmt.Errorf("таблица-приёмник %q не разрешена конфигом (PLANS_PUBLISH_TARGETS)", target)
	}
	return nil
}

func (p *mssqlPublisher) Probe(ctx context.Context, target string, rows []PublishRow) (float64, error) {
	if err := p.checkTarget(target); err != nil {
		return 0, err
	}
	var sum float64
	for _, r := range rows {
		var v sql.NullFloat64
		q := fmt.Sprintf(`SELECT SUM([Значение]) FROM %s
			WHERE [Параметр] = @p1 AND [Страна] = @p2 AND [КодЦФО] = @p3
			  AND [КодPL] = @p4 AND [Дата] = @p5`, target)
		if err := p.db.QueryRowContext(ctx, q, r.Param, r.Country,
			fmt.Sprint(r.CodeCFO), fmt.Sprint(r.CodePL), r.Date).Scan(&v); err != nil {
			return 0, err
		}
		if v.Valid {
			sum += v.Float64
		}
	}
	return round4(sum), nil
}

func (p *mssqlPublisher) Write(ctx context.Context, target string, rows []PublishRow) (int, float64, error) {
	if err := p.checkTarget(target); err != nil {
		return 0, 0, err
	}
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	delQ := fmt.Sprintf(`DELETE FROM %s
		WHERE [Параметр] = @p1 AND [Страна] = @p2 AND [КодЦФО] = @p3
		  AND [КодPL] = @p4 AND [Дата] = @p5`, target)
	insQ := fmt.Sprintf(`INSERT INTO %s
		([Параметр],[Страна],[КодЦФО],[ГруппыЦФО1],[ГруппыЦФО2],[ЦФО],[КодPL],[Дата],[Значение])
		VALUES (@p1,@p2,@p3,@p4,@p5,@p6,@p7,@p8,@p9)`, target)

	written := 0
	for _, r := range rows {
		if _, err := tx.ExecContext(ctx, delQ, r.Param, r.Country,
			fmt.Sprint(r.CodeCFO), fmt.Sprint(r.CodePL), r.Date); err != nil {
			return written, 0, fmt.Errorf("очистка ключа (%s/%d/%s): %w", r.Param, r.CodeCFO, r.Date.Format("2006-01-02"), err)
		}
		if _, err := tx.ExecContext(ctx, insQ, r.Param, r.Country, fmt.Sprint(r.CodeCFO),
			r.GroupCFO1, r.GroupCFO2, r.CFOName, fmt.Sprint(r.CodePL), r.Date, r.Value); err != nil {
			return written, 0, fmt.Errorf("запись строки (%s/%d/%s): %w", r.Param, r.CodeCFO, r.Date.Format("2006-01-02"), err)
		}
		written++
	}
	if err := tx.Commit(); err != nil {
		return written, 0, err
	}
	sum, err := p.Probe(ctx, target, rows)
	return written, sum, err
}

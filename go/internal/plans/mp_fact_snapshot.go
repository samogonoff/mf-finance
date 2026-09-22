package plans

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Снапшот факта маркетплейсов (правки ТЗ 02.09.2026, часть 2 и МП §12/§13).
//
// Правка: «Данные за отчётный месяц готовятся не раньше 20-го числа текущего.
// Форма берёт факт за прошлый месяц. Читать вживую нельзя — в динамике данные
// плавают. Готовим снапшот: отдельная таблица, в которую данные питаются за
// период, форма читает её с отметкой "данные актуальны на …"».
//
// Причина, по которой live-чтение именно ЗАПРЕЩЕНО, а не «не рекомендуется»:
// больше двухсот процедур в DWH/FinDWH/MP перезаливают таблицы через TRUNCATE.
// Один и тот же запрос в разные минуты даёт разные суммы, и согласующий увидит
// не то, что видел заполняющий, — а расхождение спишут на форму.
//
// Поэтому источник формы — таблица снимка, и при отсутствии снимка форма отдаёт
// ПУСТО с отметкой «снимок не собран», а не тихо уходит в OLAP.

// Слои снимка. Один прогон собирает все три, иначе колонки формы разъедутся по
// времени сбора.
const (
	SnapLayerFact     = "fact"
	SnapLayerStrategy = "strategy"
	SnapLayerTakt     = "takt_target"
)

// MpSnapshotMeta — отметка актуальности снимка.
type MpSnapshotMeta struct {
	Year        int       `json:"year"`
	Month       int       `json:"month"`
	Layer       string    `json:"layer"`
	Segment     string    `json:"segment"`
	RowsTotal   int       `json:"rows_total"`
	RefreshedAt time.Time `json:"refreshed_at"`
	SourceNote  string    `json:"source_note,omitempty"`
	Error       string    `json:"error,omitempty"`
	// Collected — снимок собирался. Отличать от «собран и пуст» обязательно:
	// пустой снимок это ноль факта, несобранный — неизвестность.
	Collected bool `json:"collected"`
}

// MpFactSnapshot — хранилище снимка.
type MpFactSnapshot struct {
	pool *pgxpool.Pool
	src  MpFactSource
	// horizonYears — плановый горизонт в годах после года периода. Тактика ведётся
	// на год (таргеты в приёмнике покрывают месяцы до конца года), поэтому по
	// умолчанию 0 — то есть «до декабря года периода включительно». Всё, что
	// дальше, — мусор источника: правка МП §13 называет конкретный пример, месяц
	// 2027-09-01 на две строки с суммой −1119.
	horizonYears int
}

// NewMpFactSnapshot — конструктор. src — откуда питается снимок (OLAP или mock).
func NewMpFactSnapshot(pool *pgxpool.Pool, src MpFactSource) *MpFactSnapshot {
	return &MpFactSnapshot{pool: pool, src: src}
}

// Refresh — собрать снимок периода по всем слоям. Возвращает отметки по слоям.
//
// Прогон идёт слой за слоем и НЕ прерывается на ошибке одного из них: недоступная
// стратегия не должна лишить форму факта. Ошибка запоминается в отметке — это её
// и увидит пользователь рядом с колонкой.
func (s *MpFactSnapshot) Refresh(ctx context.Context, year, month int, segment string) ([]MpSnapshotMeta, error) {
	if s.pool == nil {
		return nil, errors.New("снимок факта недоступен: нет соединения с БД")
	}
	if s.src == nil {
		return nil, errors.New("снимок факта недоступен: источник не настроен")
	}
	layers := []struct {
		name string
		load func(context.Context, int, int, string) ([]FactRow, error)
		note string
	}{
		{SnapLayerFact, s.src.MpFact, "факт МП (FinDWH)"},
		{SnapLayerStrategy, s.src.MpStrategy, "стратегия (Budgeting)"},
		{SnapLayerTakt, s.src.MpTaktTarget, "таргеты тактики (FormToLoaTaktTarget)"},
	}

	var out []MpSnapshotMeta
	for _, l := range layers {
		rows, err := l.load(ctx, year, month, segment)
		if err != nil {
			log.Printf("plans: снимок факта МП (%s, %04d-%02d): %v", l.name, year, month, err)
			if merr := s.markRun(ctx, year, month, l.name, segment, 0, l.note, err.Error()); merr != nil {
				return out, merr
			}
			out = append(out, MpSnapshotMeta{
				Year: year, Month: month, Layer: l.name, Segment: segment,
				RefreshedAt: time.Now(), SourceNote: l.note, Error: err.Error(), Collected: true,
			})
			continue
		}
		rows = s.trimHorizon(rows, year, month)
		if err := s.replace(ctx, year, month, l.name, segment, rows); err != nil {
			return out, err
		}
		if err := s.markRun(ctx, year, month, l.name, segment, len(rows), l.note, ""); err != nil {
			return out, err
		}
		out = append(out, MpSnapshotMeta{
			Year: year, Month: month, Layer: l.name, Segment: segment,
			RowsTotal: len(rows), RefreshedAt: time.Now(), SourceNote: l.note, Collected: true,
		})
	}
	return out, nil
}

// trimHorizon — отсечение мусора за плановым горизонтом (правка МП §13).
// В факте встречается месяц 2027-09-01 (две строки, сумма −1119); попадая в
// форму, он превращается в необъяснимую строку периода, которого нет в плане.
func (s *MpFactSnapshot) trimHorizon(rows []FactRow, year, month int) []FactRow {
	_ = month // горизонт задан годом плана, месяц периода на него не влияет
	limitYear := year + s.horizonYears
	out := make([]FactRow, 0, len(rows))
	dropped := 0
	for _, r := range rows {
		if r.Year == 0 || r.Month == 0 {
			dropped++
			continue
		}
		if r.Year > limitYear {
			dropped++
			continue
		}
		out = append(out, r)
	}
	if dropped > 0 {
		log.Printf("plans: снимок факта МП — отсечено строк за плановым горизонтом: %d", dropped)
	}
	return out
}

// replace — переписать слой снимка одной транзакцией: половинчатый снимок хуже
// отсутствующего, потому что выглядит собранным.
func (s *MpFactSnapshot) replace(ctx context.Context, year, month int, layer, segment string, rows []FactRow) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM mp_fact_snapshot
		 WHERE period_year = $1 AND period_month = $2 AND layer = $3 AND segment = $4`,
		year, month, layer, segment); err != nil {
		return err
	}
	for _, r := range rows {
		if _, err := tx.Exec(ctx, `
			INSERT INTO mp_fact_snapshot (period_year, period_month, layer, segment,
				code_cfo, name_cfo, code_pl, currency, amount)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
			ON CONFLICT (period_year, period_month, layer, segment, code_cfo, code_pl)
			DO UPDATE SET amount = EXCLUDED.amount, name_cfo = EXCLUDED.name_cfo,
			              currency = EXCLUDED.currency`,
			r.Year, r.Month, layer, segment, r.CodeCFO, r.NameCFO, r.CodePL,
			currencyOrDefault(r.Currency), r.Amount); err != nil {
			return fmt.Errorf("снимок (%s, ЦФО %d, статья %d): %w", layer, r.CodeCFO, r.CodePL, err)
		}
	}
	return tx.Commit(ctx)
}

func (s *MpFactSnapshot) markRun(ctx context.Context, year, month int, layer, segment string, rows int, note, errText string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mp_fact_snapshot_run (period_year, period_month, layer, segment,
			rows_total, refreshed_at, source_note, error_text)
		VALUES ($1,$2,$3,$4,$5,NOW(),$6,$7)
		ON CONFLICT (period_year, period_month, layer, segment)
		DO UPDATE SET rows_total = EXCLUDED.rows_total, refreshed_at = NOW(),
		              source_note = EXCLUDED.source_note, error_text = EXCLUDED.error_text`,
		year, month, layer, segment, rows, note, errText)
	return err
}

// Rows — строки слоя из снимка. Второй результат — отметка актуальности.
// Живого запроса к OLAP здесь нет и быть не должно (правка МП §12).
func (s *MpFactSnapshot) Rows(ctx context.Context, year, month int, layer, segment string) ([]FactRow, MpSnapshotMeta, error) {
	meta := MpSnapshotMeta{Year: year, Month: month, Layer: layer, Segment: segment}
	if s.pool == nil {
		return nil, meta, errors.New("снимок факта недоступен: нет соединения с БД")
	}
	err := s.pool.QueryRow(ctx, `
		SELECT rows_total, refreshed_at, source_note, error_text
		  FROM mp_fact_snapshot_run
		 WHERE period_year = $1 AND period_month = $2 AND layer = $3 AND segment = $4`,
		year, month, layer, segment).Scan(&meta.RowsTotal, &meta.RefreshedAt, &meta.SourceNote, &meta.Error)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// Снимок не собирался. Пустой результат с Collected=false — это «неизвестно»,
		// и форма обязана сказать это словами, а не показать нули.
		return nil, meta, nil
	case err != nil:
		return nil, meta, err
	}
	meta.Collected = true

	rows, err := s.pool.Query(ctx, `
		SELECT code_cfo, name_cfo, code_pl, currency, amount
		  FROM mp_fact_snapshot
		 WHERE period_year = $1 AND period_month = $2 AND layer = $3 AND segment = $4
		 ORDER BY code_cfo, code_pl`, year, month, layer, segment)
	if err != nil {
		return nil, meta, err
	}
	defer rows.Close()
	out := make([]FactRow, 0, meta.RowsTotal)
	for rows.Next() {
		r := FactRow{Year: year, Month: month, Scenario: "Факт"}
		if err := rows.Scan(&r.CodeCFO, &r.NameCFO, &r.CodePL, &r.Currency, &r.Amount); err != nil {
			return nil, meta, err
		}
		out = append(out, r)
	}
	return out, meta, rows.Err()
}

// Meta — только отметка актуальности (для шапки формы).
func (s *MpFactSnapshot) Meta(ctx context.Context, year, month int, layer, segment string) (MpSnapshotMeta, error) {
	_, meta, err := s.Rows(ctx, year, month, layer, segment)
	return meta, err
}

func currencyOrDefault(c string) string {
	if c == "" {
		return "RUB"
	}
	return c
}

package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Провайдеры справочников-подсказок для формы МП (правки ТЗ 02.09.2026 МП §9/§14).
//
// Почему справочник, а не запрос из формы: история цен MP.dbo.WB_Price_SPP_Analysis
// — 112 млн строк (по 13 млн на месяц по Ozon), и аналитик прямо требует считать
// подсказку «регламентным заданием раз в сутки». Именно это и делает общий движок
// синхронизации справочников (sync.go): интервал, журнал, версионирование строк,
// backoff при недоступном источнике. Своего расписания здесь нет по этой причине.
//
// Подсказка НЕ подставляется в поле (часть 1 правок) — она показывается рядом.

// MpHintTables — объекты источников подсказок (через конфиг, как остальные).
type MpHintTables struct {
	// SppAnalysisTable — MP.dbo.WB_Price_SPP_Analysis: доля spp (0…1) и цена.
	// Связь проверена аналитиком: 449,00 × (1 − 0,2308) = 345,37.
	SppAnalysisTable string
	// SalesTable — MP.dbo.sales: веса SALES и SALES_RUB, ключ соединения EAN.
	SalesTable string
	// PenaltyTable — DWH.dbo.wb_dimensions_penalty (2 709 437 строк, загрузка
	// свежая). ВНИМАНИЕ: копия в MP встала 24.04.2026 — её использовать нельзя.
	PenaltyTable string
}

// SppHintProvider — провайдер dir_mp_spp_hint.
type SppHintProvider struct {
	db  *sql.DB
	tbl MpHintTables
	now func() time.Time
}

// NewSppHintProvider — конструктор.
func NewSppHintProvider(db *sql.DB, tbl MpHintTables) *SppHintProvider {
	return &SppHintProvider{db: db, tbl: tbl, now: time.Now}
}

// Code — код справочника.
func (p *SppHintProvider) Code() string { return "dir_mp_spp_hint" }

// Fetch — взвешенное среднее %СПП за ПРОШЛЫЙ месяц по площадкам.
//
// Формула аналитика (МП §9): SUM(spp × SALES_RUB) / SUM(SALES_RUB), значения spp
// вне диапазона 0…100 отбрасываются. Диапазон проверяется в SQL, а не после
// выборки: мусорная строка не должна уехать в вес.
func (p *SppHintProvider) Fetch(ctx context.Context) ([]SyncRow, error) {
	if p.db == nil {
		return nil, errors.New("источник подсказки %СПП (MP.dbo.WB_Price_SPP_Analysis) не настроен")
	}
	if p.tbl.SppAnalysisTable == "" || p.tbl.SalesTable == "" {
		return nil, errors.New("не заданы PLANS_MP_SPP_TABLE / PLANS_MP_SALES_TABLE")
	}
	year, month := hintPrevMonth(p.now())
	q := fmt.Sprintf(`
		SELECT s.[CodeCFO] AS cfo,
		       SUM(a.[spp] * s.[SALES_RUB]) AS num,
		       SUM(s.[SALES_RUB])           AS den
		  FROM %s a
		  JOIN %s s ON s.[EAN] = a.[EAN]
		 WHERE YEAR(s.[Month]) = @p1 AND MONTH(s.[Month]) = @p2
		   AND a.[spp] >= 0 AND a.[spp] <= 100
		 GROUP BY s.[CodeCFO]`, p.tbl.SppAnalysisTable, p.tbl.SalesTable)
	rows, err := p.db.QueryContext(ctx, q, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	asOf := fmt.Sprintf("%04d-%02d", year, month)
	out := make([]SyncRow, 0, 32)
	for rows.Next() {
		var cfo sql.NullInt64
		var num, den sql.NullFloat64
		if err := rows.Scan(&cfo, &num, &den); err != nil {
			return nil, err
		}
		if !cfo.Valid || !den.Valid || den.Float64 == 0 {
			continue
		}
		spp := num.Float64 / den.Float64
		// В источнике доля лежит и как 0…1, и как процент 0…100 — нормализуем к доле.
		if spp > 1 {
			spp /= 100
		}
		out = append(out, SyncRow{
			ExternalID: strconv.FormatInt(cfo.Int64, 10),
			Payload: map[string]any{
				"code_cfo": int(cfo.Int64),
				"spp_pct":  round4(spp),
				"as_of":    asOf,
				"source":   "MP.dbo.WB_Price_SPP_Analysis × веса MP.dbo.sales (EAN), взвешенное среднее",
				"note":     "подсказка: %СПП вводится руками (правка ТЗ МП §8)",
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("подсказка %%СПП за %s пуста — синхронизация отменена", asOf)
	}
	return out, nil
}

// PenaltyHintProvider — провайдер dir_mp_penalty_hint.
type PenaltyHintProvider struct {
	db  *sql.DB
	tbl MpHintTables
	now func() time.Time
}

// NewPenaltyHintProvider — конструктор.
func NewPenaltyHintProvider(db *sql.DB, tbl MpHintTables) *PenaltyHintProvider {
	return &PenaltyHintProvider{db: db, tbl: tbl, now: time.Now}
}

// Code — код справочника.
func (p *PenaltyHintProvider) Code() string { return "dir_mp_penalty_hint" }

// Fetch — габаритные штрафы Wildberries за прошлый месяц по площадкам.
//
// Это ЧАСТЬ штрафов, а не итог (правка МП §14): общего реестра штрафов в базах
// нет, поэтому в форме штрафы — поле ввода, а эта величина показывается рядом с
// оговоркой. Источник — DWH (в MP лежит копия, вставшая 24.04.2026).
func (p *PenaltyHintProvider) Fetch(ctx context.Context) ([]SyncRow, error) {
	if p.db == nil {
		return nil, errors.New("источник подсказки по штрафам (DWH.dbo.wb_dimensions_penalty) не настроен")
	}
	if p.tbl.PenaltyTable == "" {
		return nil, errors.New("не задан PLANS_MP_PENALTY_TABLE")
	}
	year, month := hintPrevMonth(p.now())
	q := fmt.Sprintf(`
		SELECT [CodeCFO] AS cfo, SUM([Amount]) AS amount
		  FROM %s
		 WHERE YEAR([Date]) = @p1 AND MONTH([Date]) = @p2
		 GROUP BY [CodeCFO]`, p.tbl.PenaltyTable)
	rows, err := p.db.QueryContext(ctx, q, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	asOf := fmt.Sprintf("%04d-%02d", year, month)
	out := make([]SyncRow, 0, 32)
	for rows.Next() {
		var cfo sql.NullInt64
		var amount sql.NullFloat64
		if err := rows.Scan(&cfo, &amount); err != nil {
			return nil, err
		}
		if !cfo.Valid || !amount.Valid {
			continue
		}
		out = append(out, SyncRow{
			ExternalID: strconv.FormatInt(cfo.Int64, 10),
			Payload: map[string]any{
				"code_cfo": int(cfo.Int64),
				"amount":   round4(amount.Float64),
				"as_of":    asOf,
				"source":   "DWH.dbo.wb_dimensions_penalty (не копия в MP: она встала 24.04.2026)",
				"note":     "габаритные штрафы Wildberries — часть, а не итог: общего реестра штрафов нет",
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("подсказка по штрафам за %s пуста — синхронизация отменена", asOf)
	}
	return out, nil
}

// hintPrevMonth — прошлый месяц относительно момента t. Форма берёт факт за
// прошлый месяц (правка МП §12: данные отчётного месяца готовы не раньше 20-го
// числа текущего). Отдельно от prevMonth(year, month) из mp_conditions_service.go:
// тот работает с периодом карточки, а этот — с календарным «сейчас».
func hintPrevMonth(t time.Time) (int, int) {
	return prevMonth(t.Year(), int(t.Month()))
}

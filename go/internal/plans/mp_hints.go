package plans

import (
	"context"
	"log"
	"strings"
)

// Подсказки полей ввода (правки ТЗ 02.09.2026, часть 1 «Общий принцип»).
//
// Формулировка аналитика: ориентир — форма Excel. Если значение в ней не выведено
// формулой, а занесено вручную, в веб-форме это ПОЛЕ ВВОДА. Автоподстановку не
// делаем, даже когда источник в базе найден. Источник показываем ПОДСКАЗКОЙ рядом
// с полем: фактическое значение и расхождение, без подстановки и без блокировки.
//
// Отсюда три поля ввода: %СПП и штрафы в маркетплейсах, аренда в рознице.
//
// Почему это отдельный тип, а не «положить значение в поле и пометить флагом»:
// подставленное значение через одно сохранение становится введённым, и уже никто
// не отличит его от решения планировщика. Подсказка живёт РЯДОМ со значением и
// не попадает в сохранение ни при каком стечении обстоятельств.
//
// Если позже источник подтвердится, поле переключается на автоподстановку без
// переделки формы: подсказка уже несёт и значение, и его происхождение.

// MpHintKind — что за подсказка (для рендера и для тестов).
const (
	HintSpp      = "spp"       // %СПП: взвешенное среднее за прошлый месяц
	HintPenalty  = "penalty"   // штрафы: габаритные WB из DWH
	HintRentPrev = "rent_prev" // аренда: факт предыдущего месяца по тому же ЦФО
)

// FieldHint — подсказка к полю ввода.
type FieldHint struct {
	Kind      string  `json:"kind"`
	CodeCFO   int     `json:"code_cfo"`
	BlockType string  `json:"block_type"`
	Value     float64 `json:"value"`
	// Source — откуда значение. Показывается пользователю: подсказка без
	// происхождения не позволяет решить, доверять ей или нет.
	Source string `json:"source"`
	// Note — оговорки. Для штрафов это обязательно: в источнике только
	// габаритные штрафы Wildberries, а не общий реестр (правка МП §14).
	Note string `json:"note,omitempty"`
	// AsOf — на какую дату актуальны данные источника.
	AsOf string `json:"as_of,omitempty"`
}

// HintSource — источник подсказок формы МП. Реализация читает справочники
// (наполняются провайдерами раз в сутки), а не OLAP напрямую: подсказка не должна
// стоить формe живого запроса к 112 млн строк истории цен.
type HintSource interface {
	// SppHint — фактический %СПП площадки за прошлый месяц.
	SppHint(ctx context.Context, codeCFO int) (FieldHint, bool)
	// PenaltyHint — габаритные штрафы Wildberries площадки за прошлый месяц.
	PenaltyHint(ctx context.Context, codeCFO int) (FieldHint, bool)
}

// dirHintSource — подсказки из справочников кабинета.
type dirHintSource struct{ repo dirRowsSource }

// NewHintSource — конструктор. repo=nil → подсказок нет (форма работает, поля
// остаются полями ввода — это и есть требование части 1).
func NewHintSource(repo dirRowsSource) HintSource { return &dirHintSource{repo: repo} }

func (h *dirHintSource) SppHint(ctx context.Context, codeCFO int) (FieldHint, bool) {
	row, ok := h.row(ctx, "dir_mp_spp_hint", codeCFO)
	if !ok {
		return FieldHint{}, false
	}
	v := floatAt(row, "spp_pct")
	if v <= 0 {
		return FieldHint{}, false
	}
	return FieldHint{
		Kind: HintSpp, CodeCFO: codeCFO, BlockType: BSPP, Value: v,
		Source: strOr(strAt(row, "source"), "MP.dbo.WB_Price_SPP_Analysis × веса MP.dbo.sales (EAN)"),
		Note:   strAt(row, "note"),
		AsOf:   strAt(row, "as_of"),
	}, true
}

func (h *dirHintSource) PenaltyHint(ctx context.Context, codeCFO int) (FieldHint, bool) {
	row, ok := h.row(ctx, "dir_mp_penalty_hint", codeCFO)
	if !ok {
		return FieldHint{}, false
	}
	v := floatAt(row, "amount")
	if v == 0 {
		return FieldHint{}, false
	}
	return FieldHint{
		Kind: HintPenalty, CodeCFO: codeCFO, BlockType: BCostPenalties, Value: v,
		Source: strOr(strAt(row, "source"), "DWH.dbo.wb_dimensions_penalty"),
		// Оговорка обязательна: это ЧАСТЬ штрафов, а не итог (правка МП §14).
		Note: strOr(strAt(row, "note"), "габаритные штрафы Wildberries — часть, а не итог: общего реестра штрафов нет"),
		AsOf: strAt(row, "as_of"),
	}, true
}

// row — строка справочника подсказок по коду ЦФО.
func (h *dirHintSource) row(ctx context.Context, dir string, codeCFO int) (map[string]any, bool) {
	if h == nil || h.repo == nil || codeCFO == 0 {
		return nil, false
	}
	rows, err := h.repo.Rows(ctx, dir)
	if err != nil {
		log.Printf("plans: справочник подсказок %s недоступен: %v", dir, err)
		return nil, false
	}
	for _, r := range rows {
		if intAt(r.Payload, "code_cfo") == codeCFO {
			return r.Payload, true
		}
	}
	return nil, false
}

func strOr(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

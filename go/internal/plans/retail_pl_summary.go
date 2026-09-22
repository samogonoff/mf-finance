package plans

import (
	"math"
	"sort"
	"strings"
)

// Сводка «результат ввода в BYN» — вся структура PL целиком (правки ТЗ
// 02.09.2026 §28 и часть 2 «Показатели сводки розницы»).
//
// Было: шесть выбранных показателей. Верно: как в Excel, лист «результат ввода в
// BYN» — вся структура PL, 96 строк по GroupPL + Expense + CodePL, значение
// AmountBYN, месяцы по столбцам. Блока USD не делаем: в исходном файле его нет
// (§27).
//
// Почему отдельно от ComputeRetailSummary: та сводка отвечает на вопрос «как
// период соотносится со стратегией и прошлым годом» (11 показателей, разрезы
// LFL/новые/закрытые). Эта — на вопрос «что именно уедет в PL по статьям».
// Разные вопросы и разные таблицы; сшитые в одну структуру, они дают таблицу,
// которую нельзя показать ни там, ни там.
//
// Значения — в BYN, поэтому нац. суммы пересчитываются курсом. Курс берётся из
// ФАКТА (правка §7, RetailDataSource.FactFxRate), а не из справочника: в факте
// лежит тот курс, по которому суммы уже пересчитаны.

// RetailPLRow — строка структуры PL со значениями по месяцам.
type RetailPLRow struct {
	GroupPL string `json:"group_pl"`
	CodePL  int    `json:"code_pl"`
	Expense string `json:"expense"`
	// Months — значение по номеру месяца (1..12) в BYN. Месяца, для которого
	// значения нет, в карте нет: «нет строки» и «ноль» — разные вещи.
	Months map[int]float64 `json:"months"`
	Total  float64         `json:"total"`
	// Source — откуда значение: plan (ввод/расчёт формы) | fact | strategy.
	Source string `json:"source,omitempty"`
}

// RetailPLSummary — таблица «результат ввода в BYN».
type RetailPLSummary struct {
	CardID   int64         `json:"card_id"`
	Country  string        `json:"country"`
	Year     int           `json:"year"`
	Currency string        `json:"currency"` // всегда BYN — лист именно в BYN
	FxRate   float64       `json:"fx_rate"`  // курс нац. валюты к BYN (из факта)
	FxSource string        `json:"fx_source,omitempty"`
	Months   []int         `json:"months"`
	Rows     []RetailPLRow `json:"rows"`
	Groups   []string      `json:"groups"` // порядок групп PL для заголовков
}

// RetailPLKey — ключ значения структуры PL.
type RetailPLKey struct {
	CodePL int
	Month  int
}

// RetailPLInput — вход сборки. Значения приходят В НАЦ. ВАЛЮТЕ, пересчёт в BYN
// делает сборка: курс применяется в одном месте и попадает в ответ.
type RetailPLInput struct {
	CardID   int64
	Country  string
	Year     int
	Months   []int
	FxRate   float64 // нац. → BYN (из факта); 0 → без пересчёта
	FxSource string
	// Lines — статьи PL из справочника (dir_pl_line): порядок и подписи берём
	// оттуда, а не из формы — справочник авторитетен (правка МП §16).
	Lines []PLLineRow
	// Values — суммы по (код статьи, месяц) в нац. валюте: план продаж, ФОТ, аренда.
	Values map[RetailPLKey]float64
	// Sources — откуда пришла статья (plan|fact|strategy), по коду статьи.
	Sources map[int]string
}

// ComputeRetailPLSummary — собрать лист «результат ввода в BYN».
//
// Строки отдаются ВСЕ из справочника, включая нулевые: лист Excel показывает
// структуру PL целиком, и согласующий по нему проверяет, что статья не забыта.
// Прятать нулевые строки — значит показывать разную таблицу в разные месяцы.
func ComputeRetailPLSummary(in RetailPLInput) RetailPLSummary {
	rate := in.FxRate
	if rate == 0 {
		rate = 1
	}
	out := RetailPLSummary{
		CardID: in.CardID, Country: in.Country, Year: in.Year,
		Currency: "BYN", FxRate: rate, FxSource: in.FxSource,
		Months: in.Months, Rows: make([]RetailPLRow, 0, len(in.Lines)),
	}

	seenGroup := map[string]bool{}
	for _, l := range in.Lines {
		row := RetailPLRow{
			GroupPL: l.GroupPL, CodePL: l.CodePL, Expense: l.Name,
			Months: map[int]float64{}, Source: in.Sources[l.CodePL],
		}
		for _, m := range in.Months {
			v, ok := in.Values[RetailPLKey{CodePL: l.CodePL, Month: m}]
			if !ok {
				continue
			}
			byn := retailRound2(v * rate)
			row.Months[m] = byn
			row.Total = retailRound2(row.Total + byn)
		}
		out.Rows = append(out.Rows, row)
		if l.GroupPL != "" && !seenGroup[l.GroupPL] {
			seenGroup[l.GroupPL] = true
			out.Groups = append(out.Groups, l.GroupPL)
		}
	}

	// Порядок строк — как в PL: группы в порядке справочника, внутри группы по
	// коду статьи. Сортировка стабильная, иначе таблица «прыгает» между запросами.
	groupOrder := map[string]int{}
	for i, g := range out.Groups {
		groupOrder[g] = i
	}
	sort.SliceStable(out.Rows, func(i, j int) bool {
		gi, gj := groupOrder[out.Rows[i].GroupPL], groupOrder[out.Rows[j].GroupPL]
		if gi != gj {
			return gi < gj
		}
		return out.Rows[i].CodePL < out.Rows[j].CodePL
	})
	return out
}

// RetailPLGroupTotals — итоги по группам PL (строки-заголовки листа).
func RetailPLGroupTotals(sum RetailPLSummary) map[string]float64 {
	out := map[string]float64{}
	for _, r := range sum.Rows {
		if strings.TrimSpace(r.GroupPL) == "" {
			continue
		}
		out[r.GroupPL] = retailRound2(out[r.GroupPL] + r.Total)
	}
	return out
}

// retailRound2 — деньги до копейки. Отдельно от round4 (доли/проценты):
// суммы приёмника хранятся с копейками, и округление до 4 знаков давало бы
// «копейки после копеек» в сверках.
func retailRound2(v float64) float64 { return math.Round(v*100) / 100 }

package plans

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Свод «Бюджет по ЮЛ» (этапы 1.6, 3, 4; TPL-08): колонки — юридические лица, строки — продажи по каналам и расходы по
// группам P&L, суммы в BYN. ЮЛ определяется по ЦФО строки (CompanyMF справочника ЦФО), а не по ручной таблице площадок.
// Свод собирается из готовых форм и ничего в них не меняет; ручная правка итога — отдельная корректировка с причиной
// (pl_le_adjust): итог = расчёт + активные корректировки.

// Ключи строк свода: sales:<канал>, exp:<группа PL>; итоги — leKeySalesTotal, leKeyExpTotal.
const (
	LeSectionSales = "sales"
	LeSectionExp   = "exp"
	LeSectionTotal = "total"

	leKeySalesTotal = "total:sales"
	leKeyExpTotal   = "total:exp"
	leKeyRatio      = "total:ratio"

	leNoEntity = "(без ЮЛ)"
	leNoGroup  = "Без группы"
)

// leAliases — разные написания одного юрлица в источниках (ключ — в нижнем регистре). Незнакомые названия остаются как есть
// и показываются отдельной колонкой: молча склеивать разные ЮЛ нельзя.
var leAliases = map[string]string{
	"tdmf":             "TD Mark Formelle",
	"td mark formelle": "TD Mark Formelle",
	"mfkaz":            "MF Kazakhstan",
	"mf kazakhstan":    "MF Kazakhstan",
	"mft":              "MF Tex",
	"mftex":            "MF Tex",
	"mf tex":           "MF Tex",
	"mark formelle":    "Mark Formelle",
	"mf it":            "MF IT",
	"mf shanghai":      "MF Shanghai",
	"formelle":         "Formelle",
}

// NormLegalEntity приводит название ЮЛ к единому виду.
func NormLegalEntity(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		return leNoEntity
	}
	if v, ok := leAliases[strings.ToLower(s)]; ok {
		return v
	}
	return s
}

// LeAdjust — ручная корректировка итога свода.
type LeAdjust struct {
	ID          int64      `json:"id"`
	PlID        int64      `json:"pl_id"`
	LegalEntity string     `json:"legal_entity"`
	LineKey     string     `json:"line_key"`
	AmountBYN   float64    `json:"amount_byn"`
	Reason      string     `json:"reason"`
	UserID      int64      `json:"user_id"`
	UserName    string     `json:"user_name,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	Revoked     bool       `json:"revoked"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	RevokeNote  string     `json:"revoke_reason,omitempty"`
}

// LeSvodRow — строка свода. Calc — расчёт из форм, Adj — сумма активных корректировок, Total = Calc + Adj.
type LeSvodRow struct {
	Key     string             `json:"key"`
	Section string             `json:"section"`
	Label   string             `json:"label"`
	Calc    map[string]float64 `json:"calc"`
	Adj     map[string]float64 `json:"adj"`
	Total   map[string]float64 `json:"total"`
	CalcAll float64            `json:"calc_all"`
	AdjAll  float64            `json:"adj_all"`
	TotAll  float64            `json:"total_all"`
	// Ratio — только у строки отношения: проценты по ЮЛ (nil — продаж нет, делить не на что).
	Ratio    map[string]*float64 `json:"ratio,omitempty"`
	RatioAll *float64            `json:"ratio_all,omitempty"`
	// Editable — строку можно корректировать (итоговые строки считаются).
	Editable bool `json:"editable"`
}

// LeSvod — ответ свода.
type LeSvod struct {
	PlID        int64       `json:"pl_id"`
	Year        int         `json:"year"`
	Month       int         `json:"month"`
	Window      string      `json:"window"`
	FromMonth   int         `json:"from_month"`
	ToMonth     int         `json:"to_month"`
	Currency    string      `json:"currency"`
	Legals      []string    `json:"legals"`
	Rows        []LeSvodRow `json:"rows"`
	Adjustments []LeAdjust  `json:"adjustments"`
	Warnings    []string    `json:"warnings,omitempty"`
	// Closed — этап 4 завершён: корректировки закрыты.
	Closed bool `json:"closed"`
}

// leAmount — сумма в BYN по ЮЛ и строке на входе сборки.
type leAmount struct {
	Legal string
	Key   string // sales:<канал> | exp:<группа>
	Label string
	BYN   float64
}

type leSvodInput struct {
	PlID        int64
	Year, Month int
	Window      string
	From, To    int
	Amounts     []leAmount
	Adjustments []LeAdjust
	GroupOrder  []string
	Warnings    []string
	Closed      bool
}

func leAdd(m map[string]float64, k string, v float64) { m[k] += v }

// buildLeSvod — чистая сборка свода: расчёт + корректировки, колонки ЮЛ, строки по секциям, итоги и отношение расходов к продажам.
func buildLeSvod(in leSvodInput) LeSvod {
	type line struct {
		key, section, label string
		calc, adj           map[string]float64
	}
	lines := map[string]*line{}
	legalSet := map[string]bool{}
	get := func(key, label string) *line {
		l := lines[key]
		if l == nil {
			section := LeSectionSales
			if strings.HasPrefix(key, "exp:") {
				section = LeSectionExp
			}
			l = &line{key: key, section: section, label: label, calc: map[string]float64{}, adj: map[string]float64{}}
			lines[key] = l
		}
		return l
	}
	for _, a := range in.Amounts {
		l := get(a.Key, a.Label)
		leAdd(l.calc, a.Legal, a.BYN)
		legalSet[a.Legal] = true
	}
	var active []LeAdjust
	for _, a := range in.Adjustments {
		if a.Revoked {
			continue
		}
		label := strings.TrimPrefix(strings.TrimPrefix(a.LineKey, "sales:"), "exp:")
		l := get(a.LineKey, label)
		leAdd(l.adj, a.LegalEntity, a.AmountBYN)
		legalSet[a.LegalEntity] = true
		active = append(active, a)
	}

	legals := make([]string, 0, len(legalSet))
	for l := range legalSet {
		legals = append(legals, l)
	}
	sort.Slice(legals, func(i, j int) bool {
		if (legals[i] == leNoEntity) != (legals[j] == leNoEntity) {
			return legals[j] == leNoEntity
		}
		return legals[i] < legals[j]
	})

	order := map[string]int{}
	for i, g := range in.GroupOrder {
		order[g] = i
	}
	var salesKeys, expKeys []string
	for k, l := range lines {
		if l.section == LeSectionSales {
			salesKeys = append(salesKeys, k)
		} else {
			expKeys = append(expKeys, k)
		}
	}
	sort.Strings(salesKeys)
	sort.Slice(expKeys, func(i, j int) bool {
		pi, oki := order[lines[expKeys[i]].label]
		pj, okj := order[lines[expKeys[j]].label]
		if oki != okj {
			return oki
		}
		if oki && okj && pi != pj {
			return pi < pj
		}
		return lines[expKeys[i]].label < lines[expKeys[j]].label
	})

	toRow := func(l *line) LeSvodRow {
		r := LeSvodRow{Key: l.key, Section: l.section, Label: l.label, Calc: map[string]float64{}, Adj: map[string]float64{}, Total: map[string]float64{}, Editable: true}
		for _, lg := range legals {
			c, a := l.calc[lg], l.adj[lg]
			r.Calc[lg], r.Adj[lg], r.Total[lg] = round2(c), round2(a), round2(c+a)
			r.CalcAll += c
			r.AdjAll += a
		}
		r.CalcAll, r.AdjAll = round2(r.CalcAll), round2(r.AdjAll)
		r.TotAll = round2(r.CalcAll + r.AdjAll)
		return r
	}
	sumRows := func(key, label string, rows []LeSvodRow) LeSvodRow {
		t := LeSvodRow{Key: key, Section: LeSectionTotal, Label: label, Calc: map[string]float64{}, Adj: map[string]float64{}, Total: map[string]float64{}}
		for _, r := range rows {
			for _, lg := range legals {
				t.Calc[lg] += r.Calc[lg]
				t.Adj[lg] += r.Adj[lg]
				t.Total[lg] += r.Total[lg]
			}
			t.CalcAll += r.CalcAll
			t.AdjAll += r.AdjAll
			t.TotAll += r.TotAll
		}
		for _, lg := range legals {
			t.Calc[lg], t.Adj[lg], t.Total[lg] = round2(t.Calc[lg]), round2(t.Adj[lg]), round2(t.Total[lg])
		}
		t.CalcAll, t.AdjAll, t.TotAll = round2(t.CalcAll), round2(t.AdjAll), round2(t.TotAll)
		return t
	}

	out := LeSvod{
		PlID: in.PlID, Year: in.Year, Month: in.Month, Window: in.Window, FromMonth: in.From, ToMonth: in.To,
		Currency: "BYN", Legals: legals, Warnings: in.Warnings, Adjustments: active, Closed: in.Closed,
	}
	var salesRows, expRows []LeSvodRow
	for _, k := range salesKeys {
		salesRows = append(salesRows, toRow(lines[k]))
	}
	for _, k := range expKeys {
		expRows = append(expRows, toRow(lines[k]))
	}
	salesTotal := sumRows(leKeySalesTotal, "Итого продажи", salesRows)
	expTotal := sumRows(leKeyExpTotal, "Итого расходы", expRows)
	ratio := LeSvodRow{Key: leKeyRatio, Section: LeSectionTotal, Label: "Расходы к продажам, %", Ratio: map[string]*float64{}}
	pct := func(exp, sales float64) *float64 {
		if sales <= 0 {
			return nil
		}
		v := round2(exp / sales * 100)
		return &v
	}
	for _, lg := range legals {
		ratio.Ratio[lg] = pct(expTotal.Total[lg], salesTotal.Total[lg])
	}
	ratio.RatioAll = pct(expTotal.TotAll, salesTotal.TotAll)

	out.Rows = append(out.Rows, salesRows...)
	out.Rows = append(out.Rows, salesTotal)
	out.Rows = append(out.Rows, expRows...)
	out.Rows = append(out.Rows, expTotal, ratio)
	return out
}

// LeAdjustRepo — хранилище корректировок.
type LeAdjustRepo interface {
	List(ctx context.Context, plID int64) ([]LeAdjust, error)
	Add(ctx context.Context, a LeAdjust) (int64, error)
	Revoke(ctx context.Context, plID, id, userID int64, reason string) (LeAdjust, error)
}

// validateLeAdjust — проверка корректировки до записи.
func validateLeAdjust(a LeAdjust) error {
	if strings.TrimSpace(a.LegalEntity) == "" {
		return fmt.Errorf("укажите юридическое лицо")
	}
	if !strings.HasPrefix(a.LineKey, "sales:") && !strings.HasPrefix(a.LineKey, "exp:") {
		return fmt.Errorf("строка свода не распознана: %q", a.LineKey)
	}
	if strings.TrimSpace(a.Reason) == "" {
		return fmt.Errorf("укажите причину корректировки")
	}
	if a.AmountBYN == 0 {
		return fmt.Errorf("сумма корректировки не может быть нулевой")
	}
	return nil
}

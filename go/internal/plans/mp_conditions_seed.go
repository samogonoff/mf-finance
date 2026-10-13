package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Первичное заполнение долей статей из факта (замечание финансиста 13.10.2026).
//
// Реестр «Условия площадки» в первый период пуст, а «копировать из прошлого
// месяца» нечего — форма показывает «доля не задана» во всех статьях. Доли за
// прошлые периоды по ТЗ §3.1 остаются производными и в реестр САМИ не
// переносятся, поэтому заполнение — явное действие пользователя, а не подстановка:
//   - заполняются только площадки, у которых долей ещё нет (введённое не трогаем);
//   - запись помечается обоснованием «первичное заполнение из факта …»;
//   - %СПП и наценки НЕ заполняются: %СПП по правкам ТЗ (МП §8) вводится руками,
//     а себестоимости по отпускным ценам (8006) в факте нет с 2023-12. Пустые
//     значения ловит МП-01, так что недозаполненные условия не уйдут на согласование.
//
// Источник — ОПУ-витрина FinDWH.dbo.Table_Fin_PL (сценарий «факт»): в
// Budgeting.dbo.FormToLoadFact статей прямых затрат нет вообще (probe 13.10.2026),
// а в витрине есть все статьи формы по ЦФО площадок. База доли — статья 1006
// (продажи по цене площадки без НДС), та же, что у inverse-каскада.

// mpSeedMonths — сколько закрытых месяцев усредняется. Один месяц у мелких
// площадок даёт шум (разовая рекламная кампания = доля 30 %), три — сглаживают.
const mpSeedMonths = 3

// mpSeedLookback — глубина поиска закрытых месяцев перед периодом карточки.
const mpSeedLookback = 6

// mpSeedCompleteRatio — месяц считается закрытым, если строк факта в нём не
// меньше этой доли от самого полного месяца окна. Незакрытый месяц в витрине
// виден как «половина площадок без статей» (сентябрь-2026 на 13.10: 4–5 ЦФО из 15).
const mpSeedCompleteRatio = 0.8

// mpSeedBasePL — база доли: продажи по цене площадки без НДС.
const mpSeedBasePL = 1006

// MpArticleFactRow — сумма статьи площадки за месяц (BYN, знак витрины).
type MpArticleFactRow struct {
	CodeCFO int
	CodePL  int
	Year    int
	Month   int
	Amount  float64
	Rows    int // строк витрины за этой суммой — мера «закрытости» месяца
}

// MpArticleFactSource — факт по статьям площадок.
type MpArticleFactSource interface {
	MpArticleFact(ctx context.Context, months [][2]int, cfos, pls []int) ([]MpArticleFactRow, error)
	SourceName() string
}

// NewMpArticleFactSource — mock при mock=true или без MSSQL, иначе витрина ОПУ.
func NewMpArticleFactSource(mock bool, db *sql.DB, table string) MpArticleFactSource {
	if mock || db == nil || strings.TrimSpace(table) == "" {
		return mockMpArticleFact{}
	}
	return &olapMpArticleFact{db: db, table: table}
}

type olapMpArticleFact struct {
	db    *sql.DB
	table string
}

func (s *olapMpArticleFact) SourceName() string { return s.table }

func (s *olapMpArticleFact) MpArticleFact(ctx context.Context, months [][2]int, cfos, pls []int) ([]MpArticleFactRow, error) {
	if len(months) == 0 || len(cfos) == 0 || len(pls) == 0 {
		return nil, nil
	}
	dates := make([]string, 0, len(months))
	for _, ym := range months {
		dates = append(dates, fmt.Sprintf("'%04d-%02d-01'", ym[0], ym[1]))
	}
	// Все значения в запросе — числа и даты, собранные из int, поэтому
	// конкатенация безопасна; имя витрины приходит из конфига, не от клиента.
	q := fmt.Sprintf(`
		SELECT TRY_CONVERT(int,[CodeCFO]) AS cfo, TRY_CONVERT(int,[CodePL]) AS pl,
		       YEAR([Month]) AS y, MONTH([Month]) AS m, SUM([AmountBYN]) AS amt, COUNT(*) AS n
		  FROM %s WITH (NOLOCK)
		 WHERE [Scenario] = N'факт'
		   AND [Month] IN (%s)
		   AND TRY_CONVERT(int,[CodeCFO]) IN (%s)
		   AND TRY_CONVERT(int,[CodePL]) IN (%s)
		 GROUP BY TRY_CONVERT(int,[CodeCFO]), TRY_CONVERT(int,[CodePL]), YEAR([Month]), MONTH([Month])`,
		s.table, strings.Join(dates, ","), intList(cfos), intList(pls))
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MpArticleFactRow, 0, 256)
	for rows.Next() {
		var r MpArticleFactRow
		var amt sql.NullFloat64
		if err := rows.Scan(&r.CodeCFO, &r.CodePL, &r.Year, &r.Month, &amt, &r.Rows); err != nil {
			return nil, err
		}
		r.Amount = amt.Float64
		out = append(out, r)
	}
	return out, rows.Err()
}

// mockMpArticleFact — фикстура для PLANS_MOCK=1: доли порядка августа-2026.
type mockMpArticleFact struct{}

func (mockMpArticleFact) SourceName() string { return "mock" }

func (mockMpArticleFact) MpArticleFact(_ context.Context, months [][2]int, cfos, pls []int) ([]MpArticleFactRow, error) {
	shares := map[int]float64{64: 0.13, 52: 0.035, 54: 0.025, 13: 0.06, 58: 0.02, 66: 0.004}
	want := map[int]bool{}
	for _, pl := range pls {
		want[pl] = true
	}
	out := make([]MpArticleFactRow, 0)
	for _, ym := range months {
		for i, cfo := range cfos {
			sales := 1_000_000.0 * float64(i+1)
			out = append(out, MpArticleFactRow{CodeCFO: cfo, CodePL: mpSeedBasePL, Year: ym[0], Month: ym[1], Amount: -sales, Rows: 100})
			for pl, sh := range shares {
				if want[pl] {
					out = append(out, MpArticleFactRow{CodeCFO: cfo, CodePL: pl, Year: ym[0], Month: ym[1], Amount: sales * sh, Rows: 10})
				}
			}
		}
	}
	return out, nil
}

// mpSeedBlocks — статья PL → строка долей. Штрафы — отдельная строка формы со
// своим источником (FINDWH), а статья 66 витрины целиком уходит в «прочие
// удержания и компенсации»: иначе штрафы посчитались бы дважды.
func mpSeedBlocks() map[int]string {
	return map[int]string{
		64: BCostAgent, 51: BCostFreight, 52: BCostLogTransport, 54: BCostLogWarehouse,
		13: BCostAds, 15: BCostAdsSocial, 45: BCostPackaging, 58: BCostAcquiring,
		48: BCostIT, 66: BCostOther,
	}
}

// mpSeedCandidateMonths — месяцы перед периодом карточки, от ближнего к дальнему.
func mpSeedCandidateMonths(year, month int) [][2]int {
	out := make([][2]int, 0, mpSeedLookback)
	y, m := year, month
	for i := 0; i < mpSeedLookback; i++ {
		y, m = prevMonth(y, m)
		out = append(out, [2]int{y, m})
	}
	return out
}

// mpSeedClosedMonths — последние n закрытых месяцев по наполненности витрины.
// Календарь здесь не помогает: он закрывает месяц по утверждению ПЛАНА, а
// вопрос — догружен ли ФАКТ.
func mpSeedClosedMonths(candidates [][2]int, rows []MpArticleFactRow, n int) [][2]int {
	count := map[[2]int]int{}
	maxRows := 0
	for _, r := range rows {
		k := [2]int{r.Year, r.Month}
		count[k] += r.Rows
		if count[k] > maxRows {
			maxRows = count[k]
		}
	}
	out := make([][2]int, 0, n)
	if maxRows == 0 {
		return out
	}
	for _, ym := range candidates {
		if len(out) == n {
			break
		}
		if float64(count[ym]) >= mpSeedCompleteRatio*float64(maxRows) {
			out = append(out, ym)
		}
	}
	return out
}

// MpSeedPlatform — итог заполнения по одной площадке.
type MpSeedPlatform struct {
	CodeCFO int                `json:"code_cfo"`
	NameCFO string             `json:"name_cfo"`
	Status  string             `json:"status"` // filled | skipped
	Reason  string             `json:"reason,omitempty"`
	Shares  map[string]float64 `json:"shares,omitempty"`
	Base    float64            `json:"base"` // Σ продаж 1006 за окно, BYN
	// Dropped — статьи, долю которых не взяли (отрицательная вне компенсаций,
	// агентское при действующем нормативе и т.п.), с причиной.
	Dropped map[string]string `json:"dropped,omitempty"`
}

// MpSeedResult — ответ «Заполнить доли из факта».
type MpSeedResult struct {
	Months    []PeriodRef      `json:"months"`
	Source    string           `json:"source"`
	Filled    int              `json:"filled"`
	Platforms []MpSeedPlatform `json:"platforms"`
}

// computeSeedShares — доли статей площадки за окно: Σ статьи ÷ Σ |продаж 1006|.
// Взвешивание по продажам получается само: сумма сумм, а не среднее долей.
// norm > 0 — у площадки действует норматив удержаний, и агентское выводится его
// остатком (прототип, строка 85); подставлять факт поверх значит отключить это
// правило без ведома пользователя.
func computeSeedShares(rows []MpArticleFactRow, cfo int, months [][2]int, norm float64) (map[string]float64, float64, map[string]string) {
	inWindow := map[[2]int]bool{}
	for _, ym := range months {
		inWindow[ym] = true
	}
	blocks := mpSeedBlocks()
	var base float64
	sums := map[string]float64{}
	for _, r := range rows {
		if r.CodeCFO != cfo || !inWindow[[2]int{r.Year, r.Month}] {
			continue
		}
		if r.CodePL == mpSeedBasePL {
			base += r.Amount
			continue
		}
		if b, ok := blocks[r.CodePL]; ok {
			sums[b] += r.Amount
		}
	}
	// Выручка в витрине со знаком минус, расход — с плюсом.
	base = math.Abs(base)
	shares := map[string]float64{}
	dropped := map[string]string{}
	if base == 0 {
		return shares, 0, dropped
	}
	for b, v := range sums {
		share := math.Round(v/base*1e6) / 1e6 // 0,0001 п.п. — точность поля ввода
		switch {
		case share == 0:
			continue
		case b == BCostAgent && norm > 0:
			dropped[b] = fmt.Sprintf("считается остатком норматива %.1f %%", norm*100)
		case share < 0 && !mpCompensationBlock(b):
			dropped[b] = "отрицательная сумма в факте (сторно) — доля не взята"
		case share > 1:
			dropped[b] = "больше 100 % продаж — похоже на ошибку разноски"
		default:
			shares[b] = share
		}
	}
	return shares, base, dropped
}

// SeedSharesFromFact — заполнить доли статей пустых условий из факта.
func (s *MpConditionsService) SeedSharesFromFact(ctx context.Context, cardID int64, actor Principal) (MpSeedResult, error) {
	if s.articleFact == nil {
		return MpSeedResult{}, errors.New("источник факта по статьям не настроен")
	}
	card, err := s.cards.Card(ctx, cardID)
	if err != nil {
		return MpSeedResult{}, err
	}
	if !cardEditable(card) {
		return MpSeedResult{}, errors.New("период закрыт на запись: условия площадки не меняются")
	}
	scope, err := s.userScope(ctx, actor)
	if err != nil {
		return MpSeedResult{}, err
	}
	platforms := make([]MarketplaceRow, 0)
	for _, mp := range MarketplaceSeed() {
		if mpSegmentsOf(card)[mp.Segment] && mpSaveScopeAllows(scope, card.StepCode, mp.CodeCFO) {
			platforms = append(platforms, mp)
		}
	}
	if len(platforms) == 0 {
		return MpSeedResult{}, errors.New("нет доступных площадок в вашем срезе")
	}
	current, err := s.store.Conditions(ctx, card.PlID, card.Year, card.Month)
	if err != nil {
		return MpSeedResult{}, err
	}
	condByCfo := map[int]MpConditions{}
	for _, c := range current {
		condByCfo[c.CodeCFO] = c
	}

	cfos := make([]int, 0, len(platforms))
	for _, mp := range platforms {
		cfos = append(cfos, mp.CodeCFO)
	}
	pls := []int{mpSeedBasePL}
	for pl := range mpSeedBlocks() {
		pls = append(pls, pl)
	}
	sort.Ints(pls)
	candidates := mpSeedCandidateMonths(card.Year, card.Month)
	rows, err := s.articleFact.MpArticleFact(ctx, candidates, cfos, pls)
	if err != nil {
		return MpSeedResult{}, fmt.Errorf("факт по статьям недоступен: %w", err)
	}
	months := mpSeedClosedMonths(candidates, rows, mpSeedMonths)
	if len(months) == 0 {
		return MpSeedResult{}, errors.New("в факте нет закрытых месяцев за последние полгода — заполнять не из чего")
	}
	res := MpSeedResult{Source: s.articleFact.SourceName(), Platforms: make([]MpSeedPlatform, 0, len(platforms))}
	for i := len(months) - 1; i >= 0; i-- {
		res.Months = append(res.Months, PeriodRef{Year: months[i][0], Month: months[i][1]})
	}
	reason := "первичное заполнение долей из факта " + seedWindowLabel(res.Months)

	for _, mp := range platforms {
		item := MpSeedPlatform{CodeCFO: mp.CodeCFO, NameCFO: mp.NameCFO}
		cond, has := condByCfo[mp.CodeCFO]
		if has && len(cond.Shares) > 0 {
			item.Status, item.Reason = "skipped", "доли уже заданы — не перезаписываем"
			res.Platforms = append(res.Platforms, item)
			continue
		}
		norm := mpNormShareOf(mp.CodeCFO, card.Year, card.Month)
		if s.rates != nil {
			if v, ok := s.rates.NormShare(ctx, mp.CodeCFO, card.Year, card.Month); ok {
				norm = v
			}
		}
		shares, base, dropped := computeSeedShares(rows, mp.CodeCFO, months, norm)
		item.Base, item.Dropped = base, dropped
		if base == 0 {
			item.Status, item.Reason = "skipped", "нет продаж площадки в факте за эти месяцы"
			res.Platforms = append(res.Platforms, item)
			continue
		}
		if len(shares) == 0 {
			item.Status, item.Reason = "skipped", "в факте нет статей прямых затрат площадки"
			res.Platforms = append(res.Platforms, item)
			continue
		}
		in := cond
		if !has {
			in = MpConditions{CodeCFO: mp.CodeCFO, Status: "draft"}
		}
		in.Shares = shares
		in.ChangeReason = reason
		if _, err := s.SaveConditions(ctx, cardID, in, actor); err != nil {
			item.Status, item.Reason = "skipped", err.Error()
			res.Platforms = append(res.Platforms, item)
			continue
		}
		item.Status, item.Shares = "filled", shares
		res.Filled++
		res.Platforms = append(res.Platforms, item)
	}
	s.rec(ctx, actor.UserID, "mp_conditions_seed_fact", "form_card", cardID)
	return res, nil
}

// seedWindowLabel — «06–08.2026» для обоснования и сообщения пользователю.
func seedWindowLabel(months []PeriodRef) string {
	if len(months) == 0 {
		return ""
	}
	first, last := months[0], months[len(months)-1]
	if first == last {
		return fmt.Sprintf("%02d.%d", first.Month, first.Year)
	}
	if first.Year == last.Year {
		return fmt.Sprintf("%02d–%02d.%d", first.Month, last.Month, last.Year)
	}
	return fmt.Sprintf("%02d.%d–%02d.%d", first.Month, first.Year, last.Month, last.Year)
}

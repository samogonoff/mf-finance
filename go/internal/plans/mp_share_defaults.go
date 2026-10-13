package plans

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"
)

// Стратегические доли статей по умолчанию (ТЗ доработок кабинета, п. 2.1).
//
// «При открытии формы тактики по маркетплейсам стратегические доли заполнены
// значениями по умолчанию, а не пустыми; переопределение — вручную; в интерфейсе
// различать значение по умолчанию и переопределённое; расчёт от пустой доли не
// выполняется — форма явно сообщает, что доля не задана».
//
// Устройство:
//   - в реестре mp_conditions живут только РУЧНЫЕ доли (Shares) — как и раньше;
//   - доли по умолчанию приходят отдельным полем DefaultShares и в реестр не
//     пишутся. Поэтому требование ТЗ §3.1 («производные доли сами в реестр не
//     переносятся») не нарушается, а «по умолчанию / вручную» различимо по полю;
//   - каскад считает от EffectiveShares: ручная доля поверх стратегической;
//   - снимок версии карточки фиксирует DefaultShares вместе с условиями — иначе
//     утверждённые суммы поплыли бы при следующей заливке стратегии.
//
// Источник: ТЗ называет Budgeting.dbo.FormToLoadPlan / VFORMTOLOADPLAN, но по
// площадкам МП там только продажи/себестоимость/маржа — статей прямых затрат нет
// (probe 13.10.2026). Статьи стратегии лежат в ОПУ-витрине FinDWH.dbo.Table_Fin_PL,
// сценарий «НовСтрат», на каждый месяц года — по всем крупным площадкам и почти
// всем мелким. Это и есть «стратегические доли по месяцу тактики». Доля =
// статья ÷ |1006| (продажи по цене площадки без НДС) — база inverse-каскада.

// mpShareBasePL — база доли: продажи по цене площадки без НДС.
const mpShareBasePL = 1006

// mpShareDefaultsTTL — сколько держим доли периода в памяти. Стратегия меняется
// редко, а форма открывается часто; утверждённая версия читает снимок, не кэш.
const mpShareDefaultsTTL = 30 * time.Minute

// MpArticleFactRow — сумма статьи площадки за месяц (BYN, знак витрины).
type MpArticleFactRow struct {
	CodeCFO int
	CodePL  int
	Year    int
	Month   int
	Amount  float64
}

// MpArticleSource — суммы статей площадок по сценарию ОПУ-витрины.
type MpArticleSource interface {
	MpArticles(ctx context.Context, scenario string, year, month int, cfos, pls []int) ([]MpArticleFactRow, error)
	SourceName() string
}

// NewMpArticleSource — mock при mock=true или без MSSQL, иначе витрина ОПУ.
func NewMpArticleSource(mock bool, db *sql.DB, table string) MpArticleSource {
	if mock || db == nil || strings.TrimSpace(table) == "" {
		return mockMpArticles{}
	}
	return &olapMpArticles{db: db, table: table}
}

type olapMpArticles struct {
	db    *sql.DB
	table string
}

func (s *olapMpArticles) SourceName() string { return s.table }

func (s *olapMpArticles) MpArticles(ctx context.Context, scenario string, year, month int, cfos, pls []int) ([]MpArticleFactRow, error) {
	if len(cfos) == 0 || len(pls) == 0 {
		return nil, nil
	}
	// Коды ЦФО и статей — int, собраны в IN без пользовательского ввода; имя
	// витрины — из конфига. Сценарий и месяц идут параметрами.
	q := fmt.Sprintf(`
		SELECT TRY_CONVERT(int,[CodeCFO]) AS cfo, TRY_CONVERT(int,[CodePL]) AS pl, SUM([AmountBYN]) AS amt
		  FROM %s WITH (NOLOCK)
		 WHERE [Scenario] = @p1
		   AND YEAR([Month]) = @p2 AND MONTH([Month]) = @p3
		   AND TRY_CONVERT(int,[CodeCFO]) IN (%s)
		   AND TRY_CONVERT(int,[CodePL]) IN (%s)
		 GROUP BY TRY_CONVERT(int,[CodeCFO]), TRY_CONVERT(int,[CodePL])`,
		s.table, intList(cfos), intList(pls))
	rows, err := s.db.QueryContext(ctx, q, scenario, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MpArticleFactRow, 0, 128)
	for rows.Next() {
		r := MpArticleFactRow{Year: year, Month: month}
		var amt sql.NullFloat64
		if err := rows.Scan(&r.CodeCFO, &r.CodePL, &amt); err != nil {
			return nil, err
		}
		r.Amount = amt.Float64
		out = append(out, r)
	}
	return out, rows.Err()
}

// mockMpArticles — фикстура для PLANS_MOCK=1: доли порядка стратегии ноября-2026.
type mockMpArticles struct{}

func (mockMpArticles) SourceName() string { return "mock" }

func (mockMpArticles) MpArticles(_ context.Context, _ string, year, month int, cfos, pls []int) ([]MpArticleFactRow, error) {
	shares := map[int]float64{64: 0.0768, 52: 0.0457, 54: 0.0457, 13: 0.058, 58: 0.0217, 51: 0.005, 66: 0.003}
	want := map[int]bool{}
	for _, pl := range pls {
		want[pl] = true
	}
	out := make([]MpArticleFactRow, 0)
	for i, cfo := range cfos {
		sales := 1_000_000.0 * float64(i+1)
		out = append(out, MpArticleFactRow{CodeCFO: cfo, CodePL: mpShareBasePL, Year: year, Month: month, Amount: sales})
		for pl, sh := range shares {
			if want[pl] {
				out = append(out, MpArticleFactRow{CodeCFO: cfo, CodePL: pl, Year: year, Month: month, Amount: sales * sh})
			}
		}
	}
	return out, nil
}

// mpShareBlocks — статья PL → строка долей. Штрафы — отдельная строка формы со
// своим источником (FINDWH), а статья 66 витрины целиком уходит в «прочие
// удержания и компенсации»: иначе штрафы посчитались бы дважды.
func mpShareBlocks() map[int]string {
	return map[int]string{
		64: BCostAgent, 51: BCostFreight, 52: BCostLogTransport, 54: BCostLogWarehouse,
		13: BCostAds, 15: BCostAdsSocial, 45: BCostPackaging, 58: BCostAcquiring,
		48: BCostIT, 66: BCostOther,
	}
}

// computeArticleShares — доли статей площадки: Σ статьи ÷ |Σ 1006|. Строки
// других ЦФО игнорируются. Доли, которые реестр не примет (отрицательная вне
// компенсаций, больше 100 %), не берутся: такая доля по умолчанию заблокировала
// бы форму ошибкой, которую пользователь не вводил.
func computeArticleShares(rows []MpArticleFactRow, cfo int) map[string]float64 {
	blocks := mpShareBlocks()
	var base float64
	sums := map[string]float64{}
	for _, r := range rows {
		if r.CodeCFO != cfo {
			continue
		}
		if r.CodePL == mpShareBasePL {
			base += r.Amount
			continue
		}
		if b, ok := blocks[r.CodePL]; ok {
			sums[b] += r.Amount
		}
	}
	// В факте выручка со знаком минус, в стратегии — с плюсом: база по модулю.
	base = math.Abs(base)
	shares := map[string]float64{}
	if base == 0 {
		return shares
	}
	for b, v := range sums {
		share := math.Round(v/base*1e6) / 1e6 // 0,0001 п.п. — точность поля ввода
		if share == 0 || share > 1 || (share < 0 && !mpCompensationBlock(b)) {
			continue
		}
		shares[b] = share
	}
	return shares
}

// MpShareDefaults — доли по умолчанию из стратегического сценария (с кэшем).
type MpShareDefaults struct {
	src      MpArticleSource
	scenario string

	mu    sync.Mutex
	cache map[[2]int]mpShareDefaultsEntry
}

type mpShareDefaultsEntry struct {
	byCfo    map[int]map[string]float64
	loadedAt time.Time
}

// NewMpShareDefaults — конструктор. scenario — сценарий витрины («НовСтрат»).
func NewMpShareDefaults(src MpArticleSource, scenario string) *MpShareDefaults {
	if strings.TrimSpace(scenario) == "" {
		scenario = "НовСтрат"
	}
	return &MpShareDefaults{src: src, scenario: scenario, cache: map[[2]int]mpShareDefaultsEntry{}}
}

// Label — откуда доли, для подписи в форме.
func (d *MpShareDefaults) Label(year, month int) string {
	return fmt.Sprintf("стратегия «%s» %02d.%d", d.scenario, month, year)
}

// For — доли по умолчанию площадок на месяц тактики. Ошибка источника не валит
// форму: доли остаются пустыми, и форма честно пишет «доля не задана».
func (d *MpShareDefaults) For(ctx context.Context, year, month int) map[int]map[string]float64 {
	if d == nil || d.src == nil {
		return nil
	}
	key := [2]int{year, month}
	d.mu.Lock()
	if e, ok := d.cache[key]; ok && time.Since(e.loadedAt) < mpShareDefaultsTTL {
		d.mu.Unlock()
		return e.byCfo
	}
	d.mu.Unlock()

	cfos := make([]int, 0)
	for _, mp := range MarketplaceSeed() {
		cfos = append(cfos, mp.CodeCFO)
	}
	pls := []int{mpShareBasePL}
	for pl := range mpShareBlocks() {
		pls = append(pls, pl)
	}
	rows, err := d.src.MpArticles(ctx, d.scenario, year, month, cfos, pls)
	if err != nil {
		log.Printf("plans mp: стратегические доли %02d.%d (%s) недоступны: %v", month, year, d.src.SourceName(), err)
		return nil
	}
	byCfo := map[int]map[string]float64{}
	for _, cfo := range cfos {
		if sh := computeArticleShares(rows, cfo); len(sh) > 0 {
			byCfo[cfo] = sh
		}
	}
	d.mu.Lock()
	d.cache[key] = mpShareDefaultsEntry{byCfo: byCfo, loadedAt: time.Now()}
	d.mu.Unlock()
	return byCfo
}

// Attach — условия площадок сегмента с долями по умолчанию. Площадка без строки
// реестра получает пустые условия (ID=0) только с долями по умолчанию: форма
// считает статьи по стратегии ещё до первого сохранения, а %СПП и наценки
// остаются пустыми — их ловит МП-01.
func (d *MpShareDefaults) Attach(ctx context.Context, conds map[int]MpConditions, platforms []MarketplaceRow, year, month int) map[int]MpConditions {
	defaults := d.For(ctx, year, month)
	out := make(map[int]MpConditions, len(platforms))
	for k, v := range conds {
		out[k] = v
	}
	if len(defaults) == 0 {
		return out
	}
	label := d.Label(year, month)
	for _, mp := range platforms {
		sh, ok := defaults[mp.CodeCFO]
		if !ok {
			continue
		}
		c, has := out[mp.CodeCFO]
		if !has {
			c = MpConditions{CodeCFO: mp.CodeCFO, NameCFO: mp.NameCFO, Segment: mp.Segment,
				Country: mp.Country, Year: year, Month: month, Currency: platformCurrency(mp.Country),
				Shares: map[string]float64{}}
		}
		c.DefaultShares, c.DefaultSource = sh, label
		out[mp.CodeCFO] = c
	}
	return out
}

// mpPlatformsOf — площадки сегментов карточки.
func mpPlatformsOf(card Card) []MarketplaceRow {
	segments := mpSegmentsOf(card)
	out := make([]MarketplaceRow, 0)
	for _, mp := range MarketplaceSeed() {
		if segments[mp.Segment] {
			out = append(out, mp)
		}
	}
	return out
}

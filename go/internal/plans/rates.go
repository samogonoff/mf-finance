package plans

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// Справочные величины расчёта — НДС и курсы — читаются из справочников (dir_vat,
// dir_fx_rate), а не из кода.
//
// Зачем: ТЗ МП §3.4 прямо запрещает прошивать ставку НДС в формулу («эффективная
// ставка своя у каждой площадки: 20,36 % у WB/Lamoda/Ozon, 16,62 % у Yandex
// Market… это не законодательные 20 %»), а §3.3 требует брать курс тактики «из
// справочника курсов по месяцу и валюте» и фиксировать применённый курс вместе с
// утверждённой версией. До этого в коде жили vatByCountry() и FxRateSeed().
//
// Кэш в памяти на минуту: расчёт формы дёргает ставки на каждую площадку и месяц,
// а справочники меняются редко. Источник недоступен → работаем на seed-значениях
// (форма не должна падать из-за справочника), но пишем предупреждение в лог.

// VatRow — строка dir_vat: ставка по стране и (опционально) площадке.
// CodeCFO=0 — страновой дефолт; строка площадки его переопределяет.
type VatRow struct {
	Country  string  `json:"country"`
	CodeCFO  int     `json:"code_cfo"`
	Name     string  `json:"name"`
	VatRate  float64 `json:"vat_rate"`
	SourceOf string  `json:"source"`
	Note     string  `json:"note"`
}

// Сценарии курса (колонка scenario справочника dir_fx_rate). Пустой сценарий —
// общая строка, она работает для всех сценариев, пока свой не заполнен.
const (
	FxScenarioFact   = "fact"   // курс факта месяца
	FxScenarioPlan   = "plan"   // плановый курс («план 26» файла ТО: 3,2)
	FxScenarioTactic = "tactic" // курс тактики (таблица «курс тактика» прототипа МП)
)

// FxRow — строка dir_fx_rate: курс к BYN на месяц (Year=0/Month=0 — дефолт).
type FxRow struct {
	Currency string  `json:"currency"`
	Year     int     `json:"year"`
	Month    int     `json:"month"`
	RateBYN  float64 `json:"rate_byn"`
	Scenario string  `json:"scenario"`
}

// MpNormRow — строка dir_mp_norm: НОРМАТИВ совокупных удержаний площадки с
// периодом действия. Период обязателен: у Wildberries норматив 43 % действовал
// январь–февраль 2026, с марта — 42,8 % (правка 08.09.2026 п. 8). Одно значение
// на площадку означало бы, что июньские суммы не воспроизводятся.
//
// Норматив задан к выручке ПО ЦЕНАМ МЕНЕДЖЕРА без НДС — так он записан в
// договоре с площадкой и в прототипе. База формы другая (цена площадки без НДС,
// решение звонка 07.09.2026), поэтому перед применением норматив приводится к
// базе делением на (1 − %СПП) — см. agentShareFromNorm.
type MpNormRow struct {
	CodeCFO   int     `json:"code_cfo"`
	Norm      float64 `json:"norm"`
	FromYear  int     `json:"from_year"`
	FromMonth int     `json:"from_month"`
	ToYear    int     `json:"to_year"`  // 0 — без ограничения
	ToMonth   int     `json:"to_month"` // 0 — без ограничения
	Note      string  `json:"note"`
}

// PLVatRow — нормативный НДС статьи по странам (dir_pl_line, колонки VAT_* из
// Budgeting.dbo.CodePL). Значения хранятся КОЭФФИЦИЕНТОМ «1 + ставка», как в
// источнике; наружу отдаётся ставка (NormativeVat).
type PLVatRow struct {
	CodePL int
	Coef   map[string]float64 // страна → коэффициент 1+ставка
}

// dirRowsSource — минимальный доступ к строкам справочника (реализует DirRepo).
type dirRowsSource interface {
	Rows(ctx context.Context, code string) ([]DirRow, error)
}

// RateBook — справочные ставки и курсы с кэшем.
type RateBook struct {
	repo dirRowsSource

	mu       sync.RWMutex
	loadedAt time.Time
	vat      []VatRow
	fx       []FxRow
	plVat    []PLVatRow
	norms    []MpNormRow
	ttl      time.Duration
}

// NewRateBook — конструктор. repo=nil → работа на seed-значениях (dev/тесты).
func NewRateBook(repo dirRowsSource) *RateBook {
	return &RateBook{repo: repo, ttl: time.Minute}
}

// VatSeed — значения на случай недоступного справочника: законодательные ставки
// по странам + эффективные по площадкам.
//
// Эффективные ставки — ответ аналитика от 28.08.2026 (правка ТЗ МП §1–2):
// ставка привязана к КОДУ ЦФО и считается в контуре [ALL] парой кодов
// 1 − КодPL 1006 / КодPL 1022. Прежние 20,36 % и 16,62 % из ТЗ §3.4 не
// подтвердились: основной российский контур ≈ 19,6 %, ТЕКС ≈ 18,7 %,
// Казахстан ровно 16 %, Узбекистан ровно 12 %. Из представления факта МП
// ставку получить нельзя — суммы с НДС и без НДС там совпадают до копейки.
func VatSeed() []VatRow {
	const src = "1 − КодPL 1006/1022 (ответ 28.08.2026)"
	return []VatRow{
		{Country: "BY", VatRate: 0.20, Name: "Беларусь — базовая", SourceOf: "закон"},
		{Country: "RU", VatRate: 0.20, Name: "Россия — базовая", SourceOf: "закон"},
		{Country: "KZ", VatRate: 0.12, Name: "Казахстан — базовая", SourceOf: "закон"},
		{Country: "UZ", VatRate: 0.12, Name: "Узбекистан — базовая", SourceOf: "закон"},
		// Основной российский контур.
		{Country: "RU", CodeCFO: 335, VatRate: 0.1960, Name: "Wildberries", SourceOf: src},
		{Country: "RU", CodeCFO: 336, VatRate: 0.1965, Name: "Lamoda", SourceOf: src},
		{Country: "RU", CodeCFO: 337, VatRate: 0.1960, Name: "Ozon", SourceOf: src},
		// Контур ТЕКС.
		{Country: "RU", CodeCFO: 953, VatRate: 0.1872, Name: "Детский мир", SourceOf: src},
		{Country: "RU", CodeCFO: 954, VatRate: 0.1871, Name: "Yandex Market", SourceOf: src},
		{Country: "RU", CodeCFO: 955, VatRate: 0.1868, Name: "Золотое яблоко", SourceOf: src},
		{Country: "RU", CodeCFO: 957, VatRate: 0.1870, Name: "Yandex Market ТЕКС", SourceOf: src},
		{Country: "RU", CodeCFO: 958, VatRate: 0.1869, Name: "Lamoda ТЕКС", SourceOf: src},
		{Country: "RU", CodeCFO: 990, VatRate: 0.1868, Name: "Wildberries ТЕКС", SourceOf: src},
		{Country: "RU", CodeCFO: 991, VatRate: 0.1868, Name: "Ozon ТЕКС", SourceOf: src},
		// Казахстан и Узбекистан — ровные ставки.
		{Country: "KZ", CodeCFO: 338, VatRate: 0.1600, Name: "Kaspi", SourceOf: src},
		{Country: "KZ", CodeCFO: 474, VatRate: 0.1600, Name: "Ozon KZ", SourceOf: src},
		{Country: "KZ", CodeCFO: 475, VatRate: 0.1600, Name: "Wildberries KZ", SourceOf: src},
		{Country: "UZ", CodeCFO: 339, VatRate: 0.1200, Name: "Uzmarket", SourceOf: src},
	}
}

// FxSeed — курсы к BYN на случай недоступного справочника (включая KZT/UZS,
// без которых невозможен ввод в валюте площадки).
func FxSeed() []FxRow {
	return []FxRow{
		{Currency: "BYN", RateBYN: 1.0},
		{Currency: "RUB", RateBYN: 0.0376},
		{Currency: "USD", RateBYN: 3.2},
		{Currency: "KZT", RateBYN: 0.0068},
		{Currency: "UZS", RateBYN: 0.00026},
	}
}

// MpNormSeed — нормативы удержаний площадок (правка 08.09.2026 п. 8). Фолбэк на
// недоступный справочник; значения — из согласованного файла июня 2026.
func MpNormSeed() []MpNormRow {
	return []MpNormRow{
		{CodeCFO: 335, Norm: 0.430, FromYear: 2026, FromMonth: 1, ToYear: 2026, ToMonth: 2, Note: "Wildberries, январь–февраль"},
		{CodeCFO: 335, Norm: 0.428, FromYear: 2026, FromMonth: 3, Note: "Wildberries, с марта"},
		{CodeCFO: 336, Norm: 0.340, Note: "Lamoda"},
		{CodeCFO: 337, Norm: 0.350, Note: "Ozon"},
		{CodeCFO: 954, Norm: 0.320, Note: "Yandex Market"},
	}
}

// NormShare — норматив удержаний площадки на месяц. Второй результат false, если
// норматива нет: тогда доля агентского вознаграждения вводится как обычная
// статья, а не выводится остатком.
func (b *RateBook) NormShare(ctx context.Context, cfo, year, month int) (float64, bool) {
	b.load(ctx)
	b.mu.RLock()
	rows := b.norms
	b.mu.RUnlock()
	return pickNorm(rows, cfo, year, month)
}

// pickNorm — чистый выбор норматива: строка с периодом, накрывающим месяц,
// старше строки без периода.
func pickNorm(rows []MpNormRow, cfo, year, month int) (float64, bool) {
	ym := year*12 + month
	var openEnded float64
	var hasOpen bool
	for _, r := range rows {
		if r.CodeCFO != cfo {
			continue
		}
		from := r.FromYear*12 + r.FromMonth
		to := r.ToYear*12 + r.ToMonth
		switch {
		case r.FromYear == 0 && r.ToYear == 0:
			openEnded, hasOpen = r.Norm, true
		case r.ToYear == 0:
			if ym >= from {
				return r.Norm, true
			}
		default:
			if ym >= from && ym <= to {
				return r.Norm, true
			}
		}
	}
	if hasOpen {
		return openEnded, true
	}
	return 0, false
}

// PLVatSeed — нормативный НДС из сида статей: единственная копия значений живёт
// в PLLineSeed, здесь только проекция «статья → страна → коэффициент».
func PLVatSeed() []PLVatRow {
	out := make([]PLVatRow, 0, 32)
	for _, l := range PLLineSeed() {
		coef := map[string]float64{}
		for country, v := range map[string]float64{
			"BY": l.VatBY, "RU": l.VatRU, "KZ": l.VatKZ, "UZ": l.VatUZ, "TR": l.VatTR,
		} {
			if v > 0 {
				coef[country] = v
			}
		}
		if len(coef) > 0 {
			out = append(out, PLVatRow{CodePL: l.CodePL, Coef: coef})
		}
	}
	return out
}

func (b *RateBook) load(ctx context.Context) ([]VatRow, []FxRow) {
	b.mu.RLock()
	fresh := time.Since(b.loadedAt) < b.ttl && (len(b.vat) > 0 || len(b.fx) > 0)
	vat, fx := b.vat, b.fx
	b.mu.RUnlock()
	if fresh {
		return vat, fx
	}
	vat, fx = VatSeed(), FxSeed()
	plVat := PLVatSeed()
	norms := MpNormSeed()
	if b.repo != nil {
		if rows, err := b.repo.Rows(ctx, "dir_vat"); err != nil {
			log.Printf("plans: справочник НДС недоступен, работаем на дефолтных ставках: %v", err)
		} else if len(rows) > 0 {
			vat = vat[:0]
			for _, r := range rows {
				vat = append(vat, VatRow{
					Country:  strAt(r.Payload, "country"),
					CodeCFO:  intAt(r.Payload, "code_cfo"),
					Name:     strAt(r.Payload, "name"),
					VatRate:  floatAt(r.Payload, "vat_rate"),
					SourceOf: strAt(r.Payload, "source"),
					Note:     strAt(r.Payload, "note"),
				})
			}
		}
		if rows, err := b.repo.Rows(ctx, "dir_fx_rate"); err != nil {
			log.Printf("plans: справочник курсов недоступен, работаем на дефолтных курсах: %v", err)
		} else if len(rows) > 0 {
			fx = fx[:0]
			for _, r := range rows {
				fx = append(fx, FxRow{
					Currency: fxCurrencyCode(strAt(r.Payload, "currency")),
					Year:     intAt(r.Payload, "year"),
					Month:    intAt(r.Payload, "month"),
					RateBYN:  floatAt(r.Payload, "rate_byn"),
					Scenario: strAt(r.Payload, "scenario"),
				})
			}
		}
		// Нормативный НДС статьи по странам (правки ТЗ 02.09.2026, часть 5).
		// Живёт в том же справочнике статей, что и подписи: одна строка на статью,
		// пять колонок стран. Недоступен — нормативной проверки просто не будет,
		// расчёт на эффективной ставке от этого не меняется.
		if rows, err := b.repo.Rows(ctx, "dir_pl_line"); err != nil {
			log.Printf("plans: справочник статей недоступен, нормативный НДС не проверяется: %v", err)
		} else {
			plVat = plVat[:0]
			for _, r := range rows {
				code := intAt(r.Payload, "code_pl")
				if code == 0 {
					continue
				}
				coef := map[string]float64{}
				for country, key := range plVatKeys {
					if v := floatAt(r.Payload, key); v > 0 {
						coef[country] = v
					}
				}
				if len(coef) > 0 {
					plVat = append(plVat, PLVatRow{CodePL: code, Coef: coef})
				}
			}
		}
	}
	if b.repo != nil {
		// Нормативы удержаний площадок с периодом действия (правка 08.09.2026 п. 8).
		if rows, err := b.repo.Rows(ctx, "dir_mp_norm"); err != nil {
			log.Printf("plans: справочник нормативов недоступен, работаем на сиде: %v", err)
		} else if len(rows) > 0 {
			norms = norms[:0]
			for _, r := range rows {
				norms = append(norms, MpNormRow{
					CodeCFO:   intAt(r.Payload, "code_cfo"),
					Norm:      floatAt(r.Payload, "norm"),
					FromYear:  intAt(r.Payload, "from_year"),
					FromMonth: intAt(r.Payload, "from_month"),
					ToYear:    intAt(r.Payload, "to_year"),
					ToMonth:   intAt(r.Payload, "to_month"),
					Note:      strAt(r.Payload, "note"),
				})
			}
		}
	}
	b.mu.Lock()
	b.vat, b.fx, b.plVat, b.norms, b.loadedAt = vat, fx, plVat, norms, time.Now()
	b.mu.Unlock()
	return vat, fx
}

// plVatKeys — страна → ключ колонки НДС в строке справочника статей.
var plVatKeys = map[string]string{
	"BY": "vat_by", "RU": "vat_ru", "KZ": "vat_kz", "UZ": "vat_uz", "TR": "vat_tr",
}

// NormativeVat — НОРМАТИВНАЯ ставка НДС статьи в стране (правка ТЗ МП §1).
// Второй результат — есть ли значение в справочнике: «нет строки» и «НДС не
// начисляется» (коэффициент 1,00 → ставка 0) различать обязательно, иначе
// проверка границей молча пропустит любую введённую ставку.
func (b *RateBook) NormativeVat(ctx context.Context, codePL int, country string) (float64, bool) {
	b.load(ctx)
	b.mu.RLock()
	rows := b.plVat
	b.mu.RUnlock()
	return pickNormativeVat(rows, codePL, country)
}

// pickNormativeVat — чистый выбор нормативной ставки (коэффициент → ставка).
func pickNormativeVat(rows []PLVatRow, codePL int, country string) (float64, bool) {
	key := strings.ToUpper(strings.TrimSpace(country))
	for _, r := range rows {
		if r.CodePL != codePL {
			continue
		}
		coef, ok := r.Coef[key]
		if !ok || coef <= 0 {
			return 0, false
		}
		// В источнике лежит «1 + ставка»; 1,00 = НДС по статье не начисляется.
		return round4(coef - 1), true
	}
	return 0, false
}

// VatExceedsNormative — введённая (или эффективная) ставка выше нормативной.
// ТЗ МП §2: нормативная служит ВЕРХНЕЙ ГРАНИЦЕЙ и проверкой, а не значением
// расчёта — применять 22 % ко всей выручке нельзя, НДС завысится примерно на
// десятую часть. Допуск 0,0001 — против шума округления справочника.
func (b *RateBook) VatExceedsNormative(ctx context.Context, codePL int, country string, rate float64) (bool, float64) {
	norm, ok := b.NormativeVat(ctx, codePL, country)
	if !ok {
		return false, 0
	}
	return rate > norm+0.0001, norm
}

// Invalidate сбрасывает кэш (после правки справочника).
func (b *RateBook) Invalidate() {
	b.mu.Lock()
	b.loadedAt = time.Time{}
	b.mu.Unlock()
}

// Vat — эффективная ставка НДС площадки: сначала строка (страна, код ЦФО), затем
// страновой дефолт, затем 20 % как последний фолбэк.
func (b *RateBook) Vat(ctx context.Context, country string, codeCFO int) float64 {
	vat, _ := b.load(ctx)
	return pickVat(vat, country, codeCFO)
}

// pickVat — чистый выбор ставки (тестируется отдельно).
func pickVat(rows []VatRow, country string, codeCFO int) float64 {
	var byCountry float64
	var haveCountry bool
	for _, r := range rows {
		if r.CodeCFO == codeCFO && codeCFO != 0 && eqCountry(r.Country, country) {
			return r.VatRate
		}
	}
	// Строка площадки без страны (данные могли прийти без country).
	for _, r := range rows {
		if r.CodeCFO == codeCFO && codeCFO != 0 && r.Country == "" {
			return r.VatRate
		}
	}
	for _, r := range rows {
		if r.CodeCFO == 0 && eqCountry(r.Country, country) {
			byCountry, haveCountry = r.VatRate, true
		}
	}
	if haveCountry {
		return byCountry
	}
	return 0.20
}

func eqCountry(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// FxRate — курс валюты к BYN на месяц вне сценария.
func (b *RateBook) FxRate(ctx context.Context, currency string, year, month int) float64 {
	return b.FxRateFor(ctx, currency, year, month, "")
}

// FxRateFor — курс валюты к BYN на месяц В СЦЕНАРИИ.
//
// Сценарий — измерение справочника, а не свойство кода: в прототипе таблица
// «курс тактика» (Списки_для_фильтров, N:Q) задана по месяцу, валюте и сценарию,
// и у тактики курс свой. Строки со сценарием старше общих, поэтому наполнение
// сценария меняет только его колонки и не трогает остальные.
func (b *RateBook) FxRateFor(ctx context.Context, currency string, year, month int, scenario string) float64 {
	_, fx := b.load(ctx)
	return pickFx(fx, currency, year, month, scenario)
}

// FxKnown — есть ли в справочнике курс этой валюты (любой сценарий). У BYN курс — единица по
// определению. Нужен там, где «курса нет» нельзя путать с единичным курсом (pickFx в этом случае
// отдаёт 1.0): свод в BYN не должен складывать суммы нац. валюты как BYN.
func (b *RateBook) FxKnown(ctx context.Context, currency string, year, month int) bool {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" || cur == "BYN" {
		return true
	}
	_, fx := b.load(ctx)
	_, ok := pickFxBy(fx, cur, year, month, func(FxRow) bool { return true })
	return ok
}

// pickFx — чистый выбор курса. Порядок: строки запрошенного сценария (месяц →
// год → дефолт), затем строки без сценария, затем ЛЮБЫЕ строки валюты, и только
// потом 1.0.
//
// Последняя ступень не «на всякий случай»: в справочнике 0034 все строки помечены
// сценарием «Тактика бюджет (таргеты)», и без неё пересчёт валют молча стал бы
// единичным — то есть суммы в BYN и USD сравнялись бы с нац. валютой.
func pickFx(rows []FxRow, currency string, year, month int, scenario string) float64 {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" || cur == "BYN" {
		return 1.0
	}
	if scenario = normalizeFxScenario(scenario); scenario != "" {
		if v, ok := pickFxBy(rows, cur, year, month, func(r FxRow) bool {
			return normalizeFxScenario(r.Scenario) == scenario
		}); ok {
			return v
		}
	}
	if v, ok := pickFxBy(rows, cur, year, month, func(r FxRow) bool {
		return strings.TrimSpace(r.Scenario) == ""
	}); ok {
		return v
	}
	if v, ok := pickFxBy(rows, cur, year, month, func(FxRow) bool { return true }); ok {
		return v
	}
	return 1.0
}

// normalizeFxScenario — сценарий строки справочника к коду. Значения приходят
// человекочитаемыми («Тактика бюджет (таргеты)»), а сравнивать нужно по смыслу.
func normalizeFxScenario(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	switch {
	case v == "":
		return ""
	case strings.Contains(v, "тактик") || strings.Contains(v, "tactic"):
		return FxScenarioTactic
	case strings.Contains(v, "факт") || strings.Contains(v, "fact"):
		return FxScenarioFact
	case strings.Contains(v, "план") || strings.Contains(v, "plan") || strings.Contains(v, "стратег"):
		return FxScenarioPlan
	default:
		return v
	}
}

// pickFxBy — выбор строки валюты, отобранной предикатом: месяц → год → дефолт.
func pickFxBy(rows []FxRow, currency string, year, month int, match func(FxRow) bool) (float64, bool) {
	var yearOnly, def float64
	for _, r := range rows {
		if r.Currency != currency || !match(r) {
			continue
		}
		switch {
		case r.Year == year && r.Month == month && year != 0:
			return r.RateBYN, true
		case r.Year == year && r.Month == 0 && year != 0:
			yearOnly = r.RateBYN
		case r.Year == 0 && r.Month == 0:
			def = r.RateBYN
		}
	}
	if yearOnly > 0 {
		return yearOnly, true
	}
	if def > 0 {
		return def, true
	}
	return 0, false
}

// Convert — пересчёт суммы между валютами через BYN-базу на конкретный месяц.
func (b *RateBook) Convert(ctx context.Context, amount float64, src, dst string, year, month int) float64 {
	return b.ConvertFor(ctx, amount, src, dst, year, month, "")
}

// ConvertFor — то же в сценарии (у тактики курс свой, см. FxRateFor).
func (b *RateBook) ConvertFor(ctx context.Context, amount float64, src, dst string, year, month int, scenario string) float64 {
	src, dst = normalizeAnyCurrency(src), normalizeAnyCurrency(dst)
	if src == dst || amount == 0 {
		return amount
	}
	rSrc := b.FxRateFor(ctx, src, year, month, scenario)
	rDst := b.FxRateFor(ctx, dst, year, month, scenario)
	if rDst == 0 {
		return amount
	}
	return amount * rSrc / rDst
}

// Snapshot — применённые курсы для фиксации в версии карточки (ТЗ МП §3.3:
// «применённый курс фиксируется вместе с утверждённой версией»).
func (b *RateBook) Snapshot(ctx context.Context, year, month int, currencies ...string) map[string]any {
	out := map[string]any{"year": year, "month": month}
	rates := map[string]float64{}
	for _, c := range currencies {
		c = normalizeAnyCurrency(c)
		if c == "" {
			continue
		}
		rates[c] = b.FxRate(ctx, c, year, month)
	}
	out["rates_byn"] = rates
	return out
}

// fxCurrencyCode — код валюты строки справочника курсов к коду модуля.
//
// В dir_fx_rate (выгрузка из учётной системы) рубль записан не как RUB, а как
// «RUR.» (текущий курс) и «RUR2» (прежний). Без приведения pickFx не находил
// RUB и молча брал 1.0: в BYN-блоке розницы РФ рубли шли один к одному, и свод
// «все четыре розницы» завышал РФ примерно в 28 раз (найдено 13.10.2026).
func fxCurrencyCode(raw string) string {
	c := strings.ToUpper(strings.TrimSpace(raw))
	switch strings.TrimRight(c, ".0123456789") {
	case "RUR", "RUB":
		return "RUB"
	}
	return c
}

// normalizeAnyCurrency — допустимые валюты модуля: BYN/RUB/USD + валюты площадок
// KZT/UZS (ТЗ МП §3.3). Пустая → RUB (валюта хранения тактики МП).
func normalizeAnyCurrency(c string) string {
	switch strings.ToUpper(strings.TrimSpace(c)) {
	case "BYN":
		return "BYN"
	case "USD":
		return "USD"
	case "KZT":
		return "KZT"
	case "UZS":
		return "UZS"
	case "":
		return "RUB"
	default:
		return "RUB"
	}
}

// ---- хелперы чтения payload справочника ----

func strAt(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func floatAt(m map[string]any, k string) float64 {
	switch v := m[k].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return 0
}

// boolAt — флаг из payload. Строковое "true" тоже принимаем: справочник
// правится руками в UI, и значение может прийти строкой.
func boolAt(m map[string]any, k string) bool {
	switch v := m[k].(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	case float64:
		return v != 0
	}
	return false
}

func intAt(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

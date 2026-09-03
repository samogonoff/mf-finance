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

// FxRow — строка dir_fx_rate: курс к BYN на месяц (Year=0/Month=0 — дефолт).
type FxRow struct {
	Currency string  `json:"currency"`
	Year     int     `json:"year"`
	Month    int     `json:"month"`
	RateBYN  float64 `json:"rate_byn"`
	Scenario string  `json:"scenario"`
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
					Currency: strings.ToUpper(strAt(r.Payload, "currency")),
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
	b.mu.Lock()
	b.vat, b.fx, b.plVat, b.loadedAt = vat, fx, plVat, time.Now()
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

// FxRate — курс валюты к BYN на месяц: строка (валюта, год, месяц), затем
// (валюта, год), затем дефолт (0,0), затем 1.0.
func (b *RateBook) FxRate(ctx context.Context, currency string, year, month int) float64 {
	_, fx := b.load(ctx)
	return pickFx(fx, currency, year, month)
}

// pickFx — чистый выбор курса.
func pickFx(rows []FxRow, currency string, year, month int) float64 {
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" || cur == "BYN" {
		return 1.0
	}
	var yearOnly, def float64
	for _, r := range rows {
		if r.Currency != cur {
			continue
		}
		switch {
		case r.Year == year && r.Month == month && year != 0:
			return r.RateBYN
		case r.Year == year && r.Month == 0 && year != 0:
			yearOnly = r.RateBYN
		case r.Year == 0 && r.Month == 0:
			def = r.RateBYN
		}
	}
	if yearOnly > 0 {
		return yearOnly
	}
	if def > 0 {
		return def
	}
	return 1.0
}

// Convert — пересчёт суммы между валютами через BYN-базу на конкретный месяц.
func (b *RateBook) Convert(ctx context.Context, amount float64, src, dst string, year, month int) float64 {
	src, dst = normalizeAnyCurrency(src), normalizeAnyCurrency(dst)
	if src == dst || amount == 0 {
		return amount
	}
	rSrc := b.FxRate(ctx, src, year, month)
	rDst := b.FxRate(ctx, dst, year, month)
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

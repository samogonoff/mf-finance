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
	ttl      time.Duration
}

// NewRateBook — конструктор. repo=nil → работа на seed-значениях (dev/тесты).
func NewRateBook(repo dirRowsSource) *RateBook {
	return &RateBook{repo: repo, ttl: time.Minute}
}

// VatSeed — значения на случай недоступного справочника: законодательные ставки
// по странам + эффективные по площадкам large (ТЗ МП §3.4).
func VatSeed() []VatRow {
	return []VatRow{
		{Country: "BY", VatRate: 0.20, Name: "Беларусь — базовая", SourceOf: "закон"},
		{Country: "RU", VatRate: 0.20, Name: "Россия — базовая", SourceOf: "закон"},
		{Country: "KZ", VatRate: 0.12, Name: "Казахстан — базовая", SourceOf: "закон"},
		{Country: "UZ", VatRate: 0.12, Name: "Узбекистан — базовая", SourceOf: "закон"},
		{Country: "RU", CodeCFO: 335, VatRate: 0.2036, Name: "Wildberries — эффективная", SourceOf: "ТЗ §3.4"},
		{Country: "RU", CodeCFO: 336, VatRate: 0.2036, Name: "Lamoda — эффективная", SourceOf: "ТЗ §3.4"},
		{Country: "RU", CodeCFO: 337, VatRate: 0.2036, Name: "Ozon — эффективная", SourceOf: "ТЗ §3.4"},
		{Country: "RU", CodeCFO: 954, VatRate: 0.1662, Name: "Yandex Market — эффективная", SourceOf: "ТЗ §3.4"},
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

func (b *RateBook) load(ctx context.Context) ([]VatRow, []FxRow) {
	b.mu.RLock()
	fresh := time.Since(b.loadedAt) < b.ttl && (len(b.vat) > 0 || len(b.fx) > 0)
	vat, fx := b.vat, b.fx
	b.mu.RUnlock()
	if fresh {
		return vat, fx
	}
	vat, fx = VatSeed(), FxSeed()
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
	}
	b.mu.Lock()
	b.vat, b.fx, b.loadedAt = vat, fx, time.Now()
	b.mu.Unlock()
	return vat, fx
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

func intAt(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

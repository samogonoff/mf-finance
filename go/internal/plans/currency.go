package plans

// Пересчёт валют формы TPL-MP (VS7). Источник «Источник_МП» отдаёт BYN/RUB/USD;
// в MVP факт-mock — RUB, поэтому переключатель шапки пересчитывает суммы через
// BYN-базу по курсам dir_fx_rate (поле «курс тактика» прототипа: сколько BYN за
// 1 единицу валюты). Помесячные курсы из dir_fx_rate — этап 2 (sync). См. SPEC §10.3.

// FxRateRow — строка dir_fx_rate: курс валюты к BYN.
type FxRateRow struct {
	Currency string  `json:"currency"`
	RateBYN  float64 `json:"rate_byn"` // сколько BYN за 1 единицу currency
}

// FxRateSeed — курсы тактики (BYN-база). Значения — порядок из прототипа
// «Списки_для_фильтров» (RUB≈0.0376, USD≈3.2). Помесячная детализация — этап 2.
func FxRateSeed() []FxRateRow {
	return []FxRateRow{
		{Currency: "BYN", RateBYN: 1.0},
		{Currency: "RUB", RateBYN: 0.0376},
		{Currency: "USD", RateBYN: 3.2},
	}
}

// fxRateBYN — курс валюты к BYN (1 для неизвестной/пустой → как BYN-нейтраль,
// для RUB-источника обрабатывается отдельно в convertAmount).
func fxRateBYN(currency string) float64 {
	for _, r := range FxRateSeed() {
		if r.Currency == currency {
			return r.RateBYN
		}
	}
	return 1.0
}

// convertAmount переводит сумму из src в dst через BYN-базу. Пустой src → RUB.
func convertAmount(amount float64, src, dst string) float64 {
	if src == "" {
		src = "RUB"
	}
	if dst == "" {
		dst = "RUB"
	}
	if src == dst {
		return amount
	}
	byn := amount * fxRateBYN(src)
	return byn / fxRateBYN(dst)
}

// convertMetrics приводит суммы метрик к целевой валюте (для отображения формы).
func convertMetrics(rows []MetricRow, dst string) []MetricRow {
	out := make([]MetricRow, len(rows))
	for i, r := range rows {
		src := r.Currency
		r.Amount = convertAmount(r.Amount, src, dst)
		r.Currency = dst
		out[i] = r
	}
	return out
}

package plans

import (
	"context"
	"log"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Справочники реестра командировок (ответ 38, миграция 0053).
//
// Нормативы держим данными и читаем на КАЖДОМ расчёте строки: суточные и
// стоимость перелёта пересматриваются, а строка, посчитанная по старому
// нормативу, не должна тихо остаться правильной на вид.

// ExpTravelNorms — нормативы, нужные расчёту строки командировки.
type ExpTravelNorms interface {
	// PerDiem — суточные страны в долларах США (ТЗ §5.2).
	PerDiem(ctx context.Context, country string, year int) (float64, bool)
	// FlightNorm — норматив перелёта в ОДНУ сторону из РБ, бел. руб. Ключ —
	// страна и город: у Узбекистана Ташкент и Фергана стоят по-разному.
	FlightNorm(ctx context.Context, country, city string) (float64, bool)
	// FxUSD — курс доллара к валюте ввода на месяц строки.
	FxUSD(ctx context.Context, currency string, year, month int) (float64, bool)
}

type pgTravelNorms struct {
	pool  *pgxpool.Pool
	rates *RateBook
}

// NewExpTravelNorms — конструктор.
func NewExpTravelNorms(pool *pgxpool.Pool, rates *RateBook) ExpTravelNorms {
	return &pgTravelNorms{pool: pool, rates: rates}
}

func (s *pgTravelNorms) PerDiem(ctx context.Context, country string, year int) (float64, bool) {
	var v *float64
	err := s.pool.QueryRow(ctx, `
		SELECT (r.payload_json->>'amount')::numeric
		  FROM plans_directory_row r
		  JOIN plans_directory d ON d.id = r.directory_id
		 WHERE d.code = 'dir_per_diem'
		   AND UPPER(r.payload_json->>'country') = UPPER($1)
		   AND COALESCE((r.payload_json->>'from_year')::int, 0) <= $2
		 ORDER BY COALESCE((r.payload_json->>'from_year')::int, 0) DESC
		 LIMIT 1`, country, year).Scan(&v)
	if err != nil || v == nil {
		return 0, false
	}
	return *v, true
}

func (s *pgTravelNorms) FlightNorm(ctx context.Context, country, city string) (float64, bool) {
	// Сначала пара «страна + город», затем строка страны без города: у РФ и КЗ
	// норматив один на страну, у УЗ — разный по городам.
	var v *float64
	err := s.pool.QueryRow(ctx, `
		SELECT (r.payload_json->>'amount')::numeric
		  FROM plans_directory_row r
		  JOIN plans_directory d ON d.id = r.directory_id
		 WHERE d.code = 'dir_flight_norm'
		   AND UPPER(r.payload_json->>'country') = UPPER($1)
		   AND (COALESCE(r.payload_json->>'city','') = '' OR LOWER(r.payload_json->>'city') = LOWER($2))
		 ORDER BY (COALESCE(r.payload_json->>'city','') = '') ASC
		 LIMIT 1`, country, strings.TrimSpace(city)).Scan(&v)
	if err != nil || v == nil {
		return 0, false
	}
	return *v, true
}

// FxUSD — курс доллара к валюте ввода. Для белорусского рубля это курс USD→BYN
// из справочника; для остальных валют пересчёт идёт через BYN.
func (s *pgTravelNorms) FxUSD(ctx context.Context, currency string, year, month int) (float64, bool) {
	if s.rates == nil {
		return 0, false
	}
	usd := s.rates.FxRate(ctx, "USD", year, month)
	if usd <= 0 {
		log.Printf("plans/exp: курс USD на %d-%02d не задан в справочнике", year, month)
		return 0, false
	}
	cur := strings.ToUpper(strings.TrimSpace(currency))
	if cur == "" || cur == "BYN" {
		return usd, true
	}
	own := s.rates.FxRate(ctx, cur, year, month)
	if own <= 0 {
		return 0, false
	}
	// usd и own заданы «белорусских рублей за единицу», поэтому доллар в валюте
	// ввода — это отношение курсов.
	return usd / own, true
}

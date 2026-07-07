package plans

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb"
)

// Коннектор «Лисы» — MSSQL-БД Gpartner (бывший FoxPro-учёт). Креды FOX_* в env
// (.env: FOX_HOST/FOX_PORT/FOX_DB/FOX_USER/FOX_PASSWORD). ТЗ §«Справочники из
// Лисы»: метод получения — представление/таблица/сборный запрос; внешний ключ
// lisa_id; инкрементальная загрузка по changed_at (здесь — полная пересборка
// ограниченного среза, инкремент придёт с changed_at-водяным знаком).
//
// ВАЖНО: go-mssqldb отдаёт numeric/char как []byte (ASCII-строки, char пробел-
// дополнен). Поэтому сканируем всё в sql.NullString и приводим типы сами.

// SyncRow — строка справочника от провайдера: внешний ключ + payload.
type SyncRow struct {
	ExternalID string
	Payload    map[string]any
}

// Provider — источник строк одного справочника (Лиса/1С/мок).
type Provider interface {
	Code() string                              // код справочника (dir_store_to…)
	Fetch(ctx context.Context) ([]SyncRow, error)
}

// LisaDB — пул соединений к Лисе (MSSQL). nil при LISA_MOCK.
type LisaDB struct{ db *sql.DB }

// OpenLisa открывает соединение к FOX_DB. Пустой host → (nil, nil): провайдеры
// уйдут в мок/ошибку, sync не падает.
func OpenLisa(host, port, dbname, user, pass string) (*LisaDB, error) {
	if host == "" {
		return nil, nil
	}
	if port == "" {
		port = "1433"
	}
	q := url.Values{}
	q.Add("database", dbname)
	q.Add("encrypt", "disable")
	q.Add("connection timeout", "15")
	dsn := (&url.URL{
		Scheme:   "sqlserver",
		User:     url.UserPassword(user, pass),
		Host:     host + ":" + port,
		RawQuery: q.Encode(),
	}).String()
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxLifetime(30 * time.Minute)
	return &LisaDB{db: db}, nil
}

// Ping проверяет доступность Лисы.
func (l *LisaDB) Ping(ctx context.Context) error {
	if l == nil || l.db == nil {
		return fmt.Errorf("lisa: not configured")
	}
	return l.db.PingContext(ctx)
}

// Close закрывает пул.
func (l *LisaDB) Close() {
	if l != nil && l.db != nil {
		_ = l.db.Close()
	}
}

func ns(v sql.NullString) string {
	if !v.Valid {
		return ""
	}
	return strings.TrimSpace(v.String)
}

func nf(v sql.NullString) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(v.String), 64)
	return f
}

// --- Провайдер магазинов (s_klient → dir_store_to) ---

type storeProvider struct{ l *LisaDB }

func (storeProvider) Code() string { return "dir_store_to" }

func (p storeProvider) Fetch(ctx context.Context) ([]SyncRow, error) {
	if p.l == nil || p.l.db == nil {
		return nil, fmt.Errorf("lisa: not configured (set FOX_* or LISA_MOCK=1)")
	}
	// Розничные точки: ISFOLDER=0 (не папки дерева) и TRADE_AREA>0 (есть площадь).
	const q = `
SELECT s.ITEM_ID, s.KOD, s.NAIM, c.NAIM AS country_name,
       s.LFL_STATUS, s.TRADE_AREA, s.CLOSED, s.FIRMA_ID
FROM s_klient s
LEFT JOIN s_country c ON c.ITEM_ID = s.COUNTRY_ID
WHERE s.ISFOLDER = 0 AND s.TRADE_AREA > 0`
	rows, err := p.l.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SyncRow, 0, 4096)
	for rows.Next() {
		var itemID, kod, naim, country, lfl, area, closed, firma sql.NullString
		if err := rows.Scan(&itemID, &kod, &naim, &country, &lfl, &area, &closed, &firma); err != nil {
			return nil, err
		}
		id := ns(itemID)
		out = append(out, SyncRow{
			ExternalID: id,
			Payload: map[string]any{
				"lisa_id":      id,
				"code_cfo":     ns(kod),
				"store_name":   ns(naim),
				"country":      domainByCountryName(ns(country)),
				"country_name": ns(country),
				"lfl_status":   ns(lfl),
				"trade_area":   nf(area),
				"closed":       ns(closed) == "1",
				"firma_id":     ns(firma),
			},
		})
	}
	return out, rows.Err()
}

// --- Провайдер курсов валют (valuta1 + valuta → dir_fx_rate) ---

type fxProvider struct{ l *LisaDB }

func (fxProvider) Code() string { return "dir_fx_rate" }

func (p fxProvider) Fetch(ctx context.Context) ([]SyncRow, error) {
	if p.l == nil || p.l.db == nil {
		return nil, fmt.Errorf("lisa: not configured (set FOX_* or LISA_MOCK=1)")
	}
	// Последний курс по каждой валюте (KRATN — кратность номинала).
	const q = `
SELECT v.NAIM, k.KURS, k.DATA, v.KRATN
FROM valuta1 k
JOIN valuta v ON v.ITEM_ID = k.VALUTA_ID
WHERE k.DATA = (SELECT MAX(k2.DATA) FROM valuta1 k2 WHERE k2.VALUTA_ID = k.VALUTA_ID)`
	rows, err := p.l.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SyncRow, 0, 32)
	for rows.Next() {
		var name, kurs, date, kratn sql.NullString
		if err := rows.Scan(&name, &kurs, &date, &kratn); err != nil {
			return nil, err
		}
		cur := ns(name)
		if cur == "" {
			continue
		}
		rate := nf(kurs)
		if kr := nf(kratn); kr > 1 {
			rate = rate / kr
		}
		out = append(out, SyncRow{
			ExternalID: cur,
			Payload: map[string]any{
				"currency":  cur,
				"rate_byn":  rate,
				"rate_date": ns(date),
			},
		})
	}
	return out, rows.Err()
}

// --- Мок-провайдер (LISA_MOCK=1): фикстуры без сети, для dev/CI ---

type mockProvider struct {
	code string
	rows []SyncRow
}

func (m mockProvider) Code() string                              { return m.code }
func (m mockProvider) Fetch(context.Context) ([]SyncRow, error) { return m.rows, nil }

// lisaMockProviders — фикстуры справочников Лисы для LISA_MOCK.
func lisaMockProviders() []Provider {
	return []Provider{
		mockProvider{code: "dir_country", rows: canonicalCountryRows()},
		mockProvider{code: "dir_store_to", rows: []SyncRow{
			{ExternalID: "5122", Payload: map[string]any{"lisa_id": "5122", "code_cfo": "335", "store_name": "Минск ТЦ ГРИН Притыцкого 156", "country": "BY", "country_name": "Беларусь", "lfl_status": "LFL", "trade_area": 506.2, "closed": false, "firma_id": "3"}},
			{ExternalID: "5749", Payload: map[string]any{"lisa_id": "5749", "code_cfo": "0", "store_name": "Минск ТЦ Avia Mall", "country": "BY", "country_name": "Беларусь", "lfl_status": "новый", "trade_area": 423.0, "closed": true, "firma_id": "3"}},
			{ExternalID: "5154", Payload: map[string]any{"lisa_id": "5154", "code_cfo": "0", "store_name": "Уфа ТРЦ Аркада", "country": "RU", "country_name": "Россия", "lfl_status": "LFL", "trade_area": 426.0, "closed": false, "firma_id": "293"}},
		}},
		mockProvider{code: "dir_fx_rate", rows: []SyncRow{
			{ExternalID: "USD", Payload: map[string]any{"currency": "USD", "rate_byn": 3.27, "rate_date": "2026-06-01"}},
			{ExternalID: "RUB", Payload: map[string]any{"currency": "RUB", "rate_byn": 0.041, "rate_date": "2026-06-01"}},
			{ExternalID: "KZT", Payload: map[string]any{"currency": "KZT", "rate_byn": 0.0066, "rate_date": "2026-06-01"}},
		}},
	}
}

// lisaProviders — live-провайдеры поверх соединения с Лисой.
func lisaProviders(l *LisaDB) []Provider {
	return []Provider{countryProvider{l: l}, storeProvider{l: l}, fxProvider{l: l}}
}

// BuildProviders выбирает набор провайдеров: мок (LISA_MOCK=1) или live-Лиса.
// При live даже с nil-соединением провайдеры регистрируются и отдают понятную
// ошибку при синхронизации (sync_status=error виден в UI) — модуль не падает.
func BuildProviders(mock bool, l *LisaDB) []Provider {
	if mock {
		return lisaMockProviders()
	}
	return lisaProviders(l)
}

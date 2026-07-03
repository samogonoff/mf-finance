package plans

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// Redis-кэш строк справочников (запрос пользователя). Чтение: Redis →
// (miss) SQL → populate → return (fallback). TTL настраивается per-справочник
// (plans_directory.cache_ttl_seconds) + глобальный дефолт. Прогрев: часовой
// тикер sync'а после синхронизации + принудительный из настроек справочника.
//
// Ключ: plans:dir:{code}. Значение — JSON []DirRow. При недоступности Redis
// слой прозрачно деградирует в прямой SQL (никогда не валит запрос).

// DirCache — кэш строк справочников.
type DirCache struct {
	rdb        *redis.Client
	pool       *pgxpool.Pool
	defaultTTL time.Duration
}

// NewDirCache — конструктор. rdb может быть nil → кэш отключён (всегда SQL).
func NewDirCache(rdb *redis.Client, pool *pgxpool.Pool, defaultTTL time.Duration) *DirCache {
	return &DirCache{rdb: rdb, pool: pool, defaultTTL: defaultTTL}
}

func cacheKey(code string) string { return "plans:dir:" + code }

// Rows возвращает строки справочника: сперва из Redis, при промахе — из SQL с
// автоматическим прогревом кэша.
func (c *DirCache) Rows(ctx context.Context, code string) ([]DirRow, error) {
	if c.rdb != nil {
		if raw, err := c.rdb.Get(ctx, cacheKey(code)).Bytes(); err == nil {
			var rows []DirRow
			if json.Unmarshal(raw, &rows) == nil {
				return rows, nil
			}
		}
	}
	return c.loadAndStore(ctx, code)
}

// Warm принудительно перечитывает справочник из SQL и кладёт в кэш.
func (c *DirCache) Warm(ctx context.Context, code string) error {
	_, err := c.loadAndStore(ctx, code)
	return err
}

// WarmAll греет все справочники (вызывается тикером после синхронизаций).
func (c *DirCache) WarmAll(ctx context.Context) {
	rows, err := c.pool.Query(ctx, `SELECT code FROM plans_directory`)
	if err != nil {
		return
	}
	var codes []string
	for rows.Next() {
		var code string
		if rows.Scan(&code) == nil {
			codes = append(codes, code)
		}
	}
	rows.Close()
	for _, code := range codes {
		_ = c.Warm(ctx, code)
	}
}

// Invalidate удаляет справочник из кэша.
func (c *DirCache) Invalidate(ctx context.Context, code string) error {
	if c.rdb == nil {
		return nil
	}
	return c.rdb.Del(ctx, cacheKey(code)).Err()
}

// loadAndStore читает строки из SQL и (если есть Redis) кладёт с per-dir TTL.
func (c *DirCache) loadAndStore(ctx context.Context, code string) ([]DirRow, error) {
	rows, err := c.queryRows(ctx, code)
	if err != nil {
		return nil, err
	}
	if c.rdb != nil {
		if raw, err := json.Marshal(rows); err == nil {
			_ = c.rdb.Set(ctx, cacheKey(code), raw, c.ttlFor(ctx, code)).Err()
		}
	}
	return rows, nil
}

func (c *DirCache) ttlFor(ctx context.Context, code string) time.Duration {
	var ttl int
	if err := c.pool.QueryRow(ctx, `SELECT cache_ttl_seconds FROM plans_directory WHERE code=$1`, code).Scan(&ttl); err == nil && ttl > 0 {
		return time.Duration(ttl) * time.Second
	}
	return c.defaultTTL
}

func (c *DirCache) queryRows(ctx context.Context, code string) ([]DirRow, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT r.id, r.payload_json
		FROM plans_directory_row r
		JOIN plans_directory d ON d.id = r.directory_id
		WHERE d.code = $1
		ORDER BY r.id`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DirRow, 0)
	for rows.Next() {
		var dr DirRow
		var raw []byte
		if err := rows.Scan(&dr.ID, &raw); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &dr.Payload)
		out = append(out, dr)
	}
	return out, rows.Err()
}

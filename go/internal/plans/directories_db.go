package plans

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB-backed редактируемые справочники (ТЗ §«UI справочников»: ручное
// добавление/редактирование manual-НСИ). Строки живут в plans_directory_row;
// при первом старте засеваются из кода (seedRows). Движок (форма/факт/ABAC)
// пока читает каноничный seed из кода — редактирование НСИ через UI это
// отдельный слой (полное связывание — следующий шаг).

// DirRow — строка справочника с БД-идентификатором (для правки/удаления).
type DirRow struct {
	ID      int64          `json:"id"`
	Payload map[string]any `json:"payload"`
}

// DirRepo — CRUD строк справочников в БД.
type DirRepo struct{ pool *pgxpool.Pool }

// NewDirRepo — конструктор.
func NewDirRepo(pool *pgxpool.Pool) *DirRepo { return &DirRepo{pool: pool} }

// editableDirs — справочники, доступные для ручного редактирования (manual).
var editableDirs = []string{
	"dir_marketplace", "dir_pl_line", "dir_cfo", "dir_fx_rate",
	"dir_cfo_group", "dir_cfo_subgroup", "dir_cfo_type", "dir_legal_entity",
}

func seedRowsAsMaps(code string) []map[string]any {
	var v any
	switch code {
	case "dir_marketplace":
		v = MarketplaceSeed()
	case "dir_pl_line":
		v = PLLineSeed()
	case "dir_cfo":
		v = CFOSeed()
	case "dir_fx_rate":
		v = FxRateSeed()
	default:
		return nil
	}
	b, _ := json.Marshal(v)
	var out []map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

// EnsureSeed заливает строки справочников в БД при первом старте (идемпотентно:
// только если у справочника ещё нет строк). Не фатально для запуска.
func (r *DirRepo) EnsureSeed(ctx context.Context) error {
	for _, code := range editableDirs {
		var dirID int64
		err := r.pool.QueryRow(ctx, `SELECT id FROM plans_directory WHERE code = $1`, code).Scan(&dirID)
		if err != nil {
			continue // справочник не заведён миграцией — пропустить
		}
		var n int
		if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM plans_directory_row WHERE directory_id = $1`, dirID).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		for _, row := range seedRowsAsMaps(code) {
			payload, _ := json.Marshal(row)
			ext := ""
			if v, ok := row["code_cfo"]; ok {
				ext = fmt.Sprintf("%v", v)
			} else if v, ok := row["code_pl"]; ok {
				ext = fmt.Sprintf("%v", v)
			}
			if _, err := r.pool.Exec(ctx, `
				INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
				VALUES ($1, $2, $3)`, dirID, ext, payload); err != nil {
				return err
			}
		}
	}
	return nil
}

// DirMeta — метаданные справочника для реестра (с признаком редактируемости).
type DirMeta struct {
	Code       string `json:"code"`
	Source     string `json:"source"`      // lisa|1c|manual|calculated
	SyncStatus string `json:"sync_status"` // ok|error|stale|never|seed
	RowCount   int    `json:"row_count"`
	Editable   bool   `json:"editable"`    // true для source=manual
}

// Directories — реестр всех справочников из БД (manual + lisa/1c read-only).
func (r *DirRepo) Directories(ctx context.Context) ([]DirMeta, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.code, d.source, d.sync_status, COUNT(rw.id)
		FROM plans_directory d
		LEFT JOIN plans_directory_row rw ON rw.directory_id = d.id
		GROUP BY d.id, d.code, d.source, d.sync_status
		ORDER BY (d.source <> 'manual'), d.code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DirMeta, 0)
	for rows.Next() {
		var m DirMeta
		if err := rows.Scan(&m.Code, &m.Source, &m.SyncStatus, &m.RowCount); err != nil {
			return nil, err
		}
		m.Editable = m.Source == "manual"
		out = append(out, m)
	}
	return out, rows.Err()
}

// Rows — строки справочника из БД (с id для правки).
func (r *DirRepo) Rows(ctx context.Context, code string) ([]DirRow, error) {
	rows, err := r.pool.Query(ctx, `
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

// UpsertRow создаёт (id=0) или обновляет строку справочника.
func (r *DirRepo) UpsertRow(ctx context.Context, code string, id int64, payload map[string]any) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		var newID int64
		err := r.pool.QueryRow(ctx, `
			INSERT INTO plans_directory_row (directory_id, payload_json)
			VALUES ((SELECT id FROM plans_directory WHERE code = $1), $2)
			RETURNING id`, code, raw).Scan(&newID)
		return newID, err
	}
	_, err = r.pool.Exec(ctx, `
		UPDATE plans_directory_row SET payload_json = $3
		WHERE id = $2 AND directory_id = (SELECT id FROM plans_directory WHERE code = $1)`,
		code, id, raw)
	return id, err
}

// DeleteRow удаляет строку справочника.
func (r *DirRepo) DeleteRow(ctx context.Context, code string, id int64) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM plans_directory_row
		WHERE id = $2 AND directory_id = (SELECT id FROM plans_directory WHERE code = $1)`, code, id)
	return err
}

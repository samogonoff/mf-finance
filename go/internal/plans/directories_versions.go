package plans

import (
	"context"
	"encoding/json"
	"time"
)

// История версий справочника (ТЗ DIR-02). Версия меняется ТОЛЬКО при реальных
// изменениях строк; каждая запись фиксирует что/когда/кем менялось.

// DirVersion — запись истории версий.
type DirVersion struct {
	ID        int64     `json:"id"`
	Version   int       `json:"version"`
	ChangedAt time.Time `json:"changed_at"`
	Source    string    `json:"source"` // sync | manual
	ChangedBy *int64    `json:"changed_by"`
	ByName    string    `json:"by_name"`
	Added     int       `json:"added"`
	Changed   int       `json:"changed"`
	Removed   int       `json:"removed"`
	Summary   string    `json:"summary"`
}

// Versions — история версий справочника (последние limit).
func (r *DirRepo) Versions(ctx context.Context, code string, limit int) ([]DirVersion, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT v.id, v.version, v.changed_at, v.source, v.changed_by,
		       COALESCE(TRIM(COALESCE(u.last_name,'')||' '||COALESCE(u.name,'')),''),
		       v.added, v.changed, v.removed, v.summary
		FROM plans_dir_version v
		JOIN plans_directory d ON d.id = v.directory_id
		LEFT JOIN users u ON u.id = v.changed_by
		WHERE d.code = $1
		ORDER BY v.version DESC, v.id DESC
		LIMIT $2`, code, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]DirVersion, 0)
	for rows.Next() {
		var v DirVersion
		if err := rows.Scan(&v.ID, &v.Version, &v.ChangedAt, &v.Source, &v.ChangedBy,
			&v.ByName, &v.Added, &v.Changed, &v.Removed, &v.Summary); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// RecordManualVersion фиксирует ручную правку: бампает версию и пишет запись.
// op: "add"|"edit"|"delete"; ext — внешний ключ/наименование затронутой строки.
func (r *DirRepo) RecordManualVersion(ctx context.Context, code string, userID int64, op, ext string) error {
	var dirID int64
	var newVer int
	if err := r.pool.QueryRow(ctx,
		`UPDATE plans_directory SET version=version+1 WHERE code=$1 RETURNING id, version`,
		code).Scan(&dirID, &newVer); err != nil {
		return err
	}
	a, c, d := 0, 0, 0
	var summary string
	switch op {
	case "add":
		a, summary = 1, "Добавлена строка "+ext
	case "delete":
		d, summary = 1, "Удалена строка "+ext
	default:
		c, summary = 1, "Изменена строка "+ext
	}
	var by *int64
	if userID != 0 {
		by = &userID
	}
	sample, _ := json.Marshal([]map[string]string{{"op": op, "id": ext}})
	_, err := r.pool.Exec(ctx, `
		INSERT INTO plans_dir_version (directory_id, version, source, changed_by, added, changed, removed, summary, diff_sample)
		VALUES ($1,$2,'manual',$3,$4,$5,$6,$7,$8)`,
		dirID, newVer, by, a, c, d, summary, sample)
	return err
}

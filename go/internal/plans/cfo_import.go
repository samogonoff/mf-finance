package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Импорт справочника ЦФО из «Справочник ЦФО и ЦЗ.xlsx» в dir_cfo (ТЗ §7.1.1).
// Берём ДВА нормализованных листа: «Code CFO магазин с площ» (розница) и
// «Code CFO отд. пр-ва» (производство/отделы). Строим иерархию
// страна → group_cfo1 → group_cfo2 → ЦФО для древовидного интерфейса.
// Страна приводится к доменному коду через единый справочник (domainByAbbr).

// findHeader ищет строку-заголовок по обязательным маркерам и возвращает
// map имя_колонки(lower)→индекс и номер строки данных.
func findHeader(rows [][]string, markers ...string) (map[string]int, int) {
	for i, r := range rows {
		hit := 0
		idx := map[string]int{}
		for c, v := range r {
			key := strings.ToLower(strings.TrimSpace(v))
			if key != "" {
				idx[key] = c
			}
		}
		for _, m := range markers {
			if _, ok := idx[strings.ToLower(m)]; ok {
				hit++
			}
		}
		if hit == len(markers) {
			return idx, i + 1
		}
	}
	return nil, -1
}

func at(r []string, i int) string {
	if i >= 0 && i < len(r) {
		return strings.TrimSpace(r[i])
	}
	return ""
}

type cfoRow struct {
	CodeCFO        string  `json:"code_cfo"`
	NameCFO        string  `json:"name_cfo"`
	GroupCFO1      string  `json:"group_cfo1"`
	GroupCFO2      string  `json:"group_cfo2"`
	Country        string  `json:"country"`
	EntityType     string  `json:"entity_type"`
	LegalEntity    string  `json:"legal_entity"`     // каноничное имя (через справочник ЮЛ)
	LegalEntityRaw string  `json:"legal_entity_raw"` // исходное значение из xlsx
	City           string  `json:"city"`
	LisaID         string  `json:"lisa_id"`
	TradeArea      float64 `json:"trade_area"`
	Top            string  `json:"top"`      // ФИО ТОП ЦФО (из xlsx)
	Director       string  `json:"director"` // = директор ЮЛ (производное)
}

// ImportCFO парсит xlsx и заменяет строки dir_cfo. Возвращает число загруженных.
func ImportCFO(ctx context.Context, pool *pgxpool.Pool, data []byte) (int, error) {
	wb, err := openXLSX(data)
	if err != nil {
		return 0, fmt.Errorf("xlsx: %w", err)
	}

	byCode := map[string]cfoRow{}
	order := []string{}
	add := func(r cfoRow) {
		if r.CodeCFO == "" || r.NameCFO == "" {
			return
		}
		if _, ok := byCode[r.CodeCFO]; !ok {
			order = append(order, r.CodeCFO)
		}
		byCode[r.CodeCFO] = r
	}

	// Лист 1 — магазины (розница).
	if rows, err := wb.Sheet("Code CFO магазин с площ"); err == nil {
		h, start := findHeader(rows, "Страна", "Название ЦФО", "ЦФО")
		if h != nil {
			for _, r := range rows[start:] {
				area, _ := strconv.ParseFloat(strings.Replace(at(r, h["кв.м."]), ",", ".", 1), 64)
				le := at(r, h["юр. лица"])
				add(cfoRow{
					CodeCFO: at(r, h["цфо"]), NameCFO: at(r, h["название цфо"]),
					GroupCFO1: "Розница", GroupCFO2: at(r, h["категория"]),
					Country: domainByAbbr(at(r, h["страна"])), EntityType: "магазин",
					LegalEntity: canonicalLegalEntity(le), LegalEntityRaw: le, City: at(r, h["город"]),
					LisaID: at(r, h["id (из лисы)"]), TradeArea: area,
				})
			}
		}
	}

	// Лист 2 — производство/отделы.
	if rows, err := wb.Sheet("Code CFO отд. пр-ва"); err == nil {
		h, start := findHeader(rows, "Страна", "Группа ЦФО", "ЦФО")
		if h != nil {
			for _, r := range rows[start:] {
				g1 := at(r, h["группа цфо"])
				entity := "отдел"
				if strings.Contains(strings.ToLower(g1), "производ") {
					entity = "производство"
				}
				le := at(r, h["юр. лица"])
				add(cfoRow{
					CodeCFO: at(r, h["цфо"]), NameCFO: at(r, h["название цфо"]),
					GroupCFO1: g1, GroupCFO2: at(r, h["группа цфо2"]),
					Country: domainByAbbr(at(r, h["страна"])), EntityType: entity,
					LegalEntity: canonicalLegalEntity(le), LegalEntityRaw: le, City: at(r, h["город"]),
					Top: at(r, h["фио топ"]),
				})
			}
		}
	}

	if len(order) == 0 {
		return 0, fmt.Errorf("в xlsx не найдено строк ЦФО (проверьте листы «Code CFO магазин с площ» / «Code CFO отд. пр-ва»)")
	}

	// Директор ЦФО = директор его ЮЛ (производное, не из xlsx). Тянем карту
	// «каноничное имя ЮЛ → директор» из dir_legal_entity.
	dirByLE := map[string]string{}
	if lr, err := pool.Query(ctx, `
		SELECT payload_json->>'name', COALESCE(payload_json->>'director','')
		FROM plans_directory_row r JOIN plans_directory d ON d.id=r.directory_id
		WHERE d.code='dir_legal_entity'`); err == nil {
		for lr.Next() {
			var name, dir string
			if lr.Scan(&name, &dir) == nil {
				dirByLE[name] = dir
			}
		}
		lr.Close()
	}
	for code, row := range byCode {
		row.Director = dirByLE[row.LegalEntity]
		byCode[code] = row
	}

	// Заменяем строки dir_cfo транзакционно.
	var dirID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM plans_directory WHERE code='dir_cfo'`).Scan(&dirID); err != nil {
		return 0, fmt.Errorf("dir_cfo не зарегистрирован: %w", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM plans_directory_row WHERE directory_id=$1`, dirID); err != nil {
		return 0, err
	}
	batch := &pgx.Batch{}
	for _, code := range order {
		payload, _ := json.Marshal(byCode[code])
		batch.Queue(`INSERT INTO plans_directory_row (directory_id, external_id, payload_json) VALUES ($1,$2,$3)`, dirID, code, payload)
	}
	br := tx.SendBatch(ctx, batch)
	for range order {
		if _, err := br.Exec(); err != nil {
			br.Close()
			return 0, err
		}
	}
	if err := br.Close(); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE plans_directory SET sync_status='ok', synced_at=NOW(), version=version+1 WHERE id=$1`, dirID); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	_ = SeedCFOSubdirs(ctx, pool, true) // пере-засеять под-справочники из новых данных
	return len(order), nil
}

// cfoSubdirField — соответствие под-справочника и поля payload dir_cfo.
var cfoSubdirField = map[string]string{
	"dir_cfo_group":    "group_cfo1",
	"dir_cfo_subgroup": "group_cfo2",
	"dir_cfo_type":     "entity_type",
}

// SeedCFOSubdirs наполняет под-справочники (Группа/Подгруппа/Тип) уникальными
// значениями из dir_cfo. force=true пересобирает; иначе только если пусто.
func SeedCFOSubdirs(ctx context.Context, pool *pgxpool.Pool, force bool) error {
	for code, field := range cfoSubdirField {
		var dirID int64
		if err := pool.QueryRow(ctx, `SELECT id FROM plans_directory WHERE code=$1`, code).Scan(&dirID); err != nil {
			continue
		}
		var n int
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM plans_directory_row WHERE directory_id=$1`, dirID).Scan(&n)
		if n > 0 && !force {
			continue
		}
		rows, err := pool.Query(ctx, `
			SELECT DISTINCT payload_json->>$1 AS v
			FROM plans_directory_row r JOIN plans_directory d ON d.id=r.directory_id
			WHERE d.code='dir_cfo' AND COALESCE(payload_json->>$1,'') <> ''
			ORDER BY v`, field)
		if err != nil {
			continue
		}
		var vals []string
		for rows.Next() {
			var v string
			if rows.Scan(&v) == nil {
				vals = append(vals, v)
			}
		}
		rows.Close()
		tx, err := pool.Begin(ctx)
		if err != nil {
			continue
		}
		_, _ = tx.Exec(ctx, `DELETE FROM plans_directory_row WHERE directory_id=$1`, dirID)
		for _, v := range vals {
			payload, _ := json.Marshal(map[string]any{"code": v, "name": v})
			_, _ = tx.Exec(ctx, `INSERT INTO plans_directory_row (directory_id, external_id, payload_json) VALUES ($1,$2,$3)`, dirID, v, payload)
		}
		_, _ = tx.Exec(ctx, `UPDATE plans_directory SET sync_status='seed', synced_at=NOW() WHERE id=$1`, dirID)
		_ = tx.Commit(ctx)
	}
	return nil
}

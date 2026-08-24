package plans

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Хранилище маппинга и журнала публикаций (миграция 0032).

type pgPublishStore struct{ pool *pgxpool.Pool }

// NewPublishStore — конструктор.
func NewPublishStore(pool *pgxpool.Pool) PublishStore { return &pgPublishStore{pool: pool} }

func (s *pgPublishStore) Mappings(ctx context.Context, formCode string) ([]PublishMapping, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, form_code, block_type, country, param_name, COALESCE(code_pl,0),
		       target_table, target_currency, aggregate, COALESCE(aggregate_cfo,0), enabled, note
		  FROM publish_mapping WHERE form_code = $1
		 ORDER BY block_type, country, param_name`, formCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublishMapping
	for rows.Next() {
		var m PublishMapping
		if err := rows.Scan(&m.ID, &m.FormCode, &m.BlockType, &m.Country, &m.ParamName,
			&m.CodePL, &m.TargetTable, &m.TargetCurrency, &m.Aggregate, &m.AggregateCFO,
			&m.Enabled, &m.Note); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *pgPublishStore) UpsertMapping(ctx context.Context, m PublishMapping, actorID int64) error {
	var actor, codePL, aggCFO any
	if actorID != 0 {
		actor = actorID
	}
	if m.CodePL > 0 {
		codePL = m.CodePL
	}
	if m.AggregateCFO > 0 {
		aggCFO = m.AggregateCFO
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO publish_mapping (form_code, block_type, country, param_name, code_pl,
			target_table, target_currency, aggregate, aggregate_cfo, enabled, note, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,NOW())
		ON CONFLICT (form_code, block_type, country, param_name, target_table, target_currency)
		DO UPDATE SET code_pl = EXCLUDED.code_pl, aggregate = EXCLUDED.aggregate,
			aggregate_cfo = EXCLUDED.aggregate_cfo, enabled = EXCLUDED.enabled,
			note = EXCLUDED.note, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
		m.FormCode, m.BlockType, m.Country, m.ParamName, codePL, m.TargetTable,
		m.TargetCurrency, m.Aggregate, aggCFO, m.Enabled, m.Note, actor)
	return err
}

func (s *pgPublishStore) LogPublish(ctx context.Context, r PublishResult, actorID int64, report []byte) error {
	var actor any
	if actorID != 0 {
		actor = actorID
	}
	if len(report) == 0 {
		report = []byte(`{}`)
	}
	key := ""
	if r.Mode == PublishModeWrite && r.Status == "ok" {
		key = publishKey(r)
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO publish_log (card_id, version_no, target, mode, idempotency_key,
			rows_total, rows_written, sum_input, sum_target, diff, status, error_text, report, finished_at, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,NOW(),$14)`,
		r.CardID, r.VersionNo, r.Target, r.Mode, key, r.RowsTotal, r.RowsWritten,
		r.SumInput, r.SumTarget, r.Diff, r.Status, r.Error, report, actor)
	return err
}

// publishKey — ключ идемпотентности успешной записи версии в приёмник.
func publishKey(r PublishResult) string {
	return "card:" + itoa(r.CardID) + ":v" + itoa(int64(r.VersionNo)) + ":" + r.Target
}

func (s *pgPublishStore) AlreadyPublished(ctx context.Context, key string) (bool, error) {
	if key == "" {
		return false, nil
	}
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM publish_log WHERE idempotency_key = $1 AND status = 'ok' AND mode = 'write'`,
		key).Scan(&n)
	return n > 0, err
}

func (s *pgPublishStore) PublishLog(ctx context.Context, cardID int64) ([]PublishLogItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, version_no, mode, target, status, rows_total, rows_written, diff, error_text, started_at
		  FROM publish_log WHERE card_id = $1 ORDER BY started_at DESC LIMIT 100`, cardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublishLogItem
	for rows.Next() {
		var it PublishLogItem
		if err := rows.Scan(&it.ID, &it.VersionNo, &it.Mode, &it.Target, &it.Status,
			&it.RowsTotal, &it.RowsOK, &it.Diff, &it.Error, &it.StartedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

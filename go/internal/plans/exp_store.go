package plans

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Хранилище формы 1.5 (миграция 0051). Только SQL: правила — в exp_service.go.

// ExpRepo — операции над сеткой формы.
type ExpRepo interface {
	EnsureInstance(ctx context.Context, in ExpInstance, actorID int64) (ExpInstance, error)
	Instance(ctx context.Context, cardID int64) (ExpInstance, error)
	SyncRows(ctx context.Context, instanceID int64, rows []ExpRow) (int, error)
	Rows(ctx context.Context, instanceID int64, f ExpFilter) ([]ExpRow, int, error)
	SaveValues(ctx context.Context, instanceID int64, cells []ExpCell, actorID int64, cardVersion int) (int, error)
	SaveReadOnly(ctx context.Context, instanceID int64, scenario string, cells []ExpCell) error
	SaveComment(ctx context.Context, instanceID int64, codeCFO, codePL int, comment string) error
	GroupTotals(ctx context.Context, instanceID int64) (map[string]float64, error)
	GroupOrder(ctx context.Context, formKind string) ([]string, error)
	Params(ctx context.Context, instanceID int64) ([]ExpParamRow, error)
	SaveParam(ctx context.Context, instanceID int64, p ExpParamRow, actorID int64) error
	SyncPLLines(ctx context.Context, lines []ExpPLLine) (int, error)
}

// ExpPLLine — статья справочника, как она приходит из источника затрат.
type ExpPLLine struct {
	CodePL  int
	Expense string
	GroupPL string
}

type expRepo struct{ pool *pgxpool.Pool }

// NewExpRepo — конструктор.
func NewExpRepo(pool *pgxpool.Pool) ExpRepo { return &expRepo{pool: pool} }

// EnsureInstance — экземпляр формы на карточку, идемпотентно.
func (s *expRepo) EnsureInstance(ctx context.Context, in ExpInstance, actorID int64) (ExpInstance, error) {
	err := s.pool.QueryRow(ctx, `
		INSERT INTO exp_instance (card_id, form_kind, country, plan_year, currency, legal_entity, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (card_id) DO UPDATE SET updated_at = NOW()
		RETURNING id, card_id, form_kind, country, plan_year, currency, legal_entity`,
		in.CardID, in.FormKind, in.Country, in.PlanYear, in.Currency, in.LegalEntity, nullID(actorID),
	).Scan(&in.ID, &in.CardID, &in.FormKind, &in.Country, &in.PlanYear, &in.Currency, &in.LegalEntity)
	return in, err
}

// Instance — экземпляр по карточке.
func (s *expRepo) Instance(ctx context.Context, cardID int64) (ExpInstance, error) {
	var in ExpInstance
	err := s.pool.QueryRow(ctx, `
		SELECT id, card_id, form_kind, country, plan_year, currency, legal_entity
		  FROM exp_instance WHERE card_id = $1`, cardID,
	).Scan(&in.ID, &in.CardID, &in.FormKind, &in.Country, &in.PlanYear, &in.Currency, &in.LegalEntity)
	return in, err
}

// SyncRows — приведение состава строк к источнику. Возвращает число строк формы.
//
// Строки только ДОБАВЛЯЮТСЯ и обновляют атрибуты; лишние не удаляются. Причина:
// удаление строки унесло бы с собой уже введённые значения (ON DELETE CASCADE),
// а исчезновение ЦФО из «быстрого» среза источника — обычное дело. Расхождение
// состава ловит контроль E-07 (ТЗ §8), и это решение человека, а не синхронизации.
func (s *expRepo) SyncRows(ctx context.Context, instanceID int64, rows []ExpRow) (int, error) {
	if len(rows) > 0 {
		b := &pgx.Batch{}
		for _, r := range rows {
			b.Queue(`
				INSERT INTO exp_row (instance_id, code_cfo, code_pl, cfo, group_cfo1, group_cfo2,
				                     group_pl, expense, legal_entity, country_company, store_type, reg_manager)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
				ON CONFLICT (instance_id, code_cfo, code_pl) DO UPDATE SET
				    cfo = EXCLUDED.cfo, group_cfo1 = EXCLUDED.group_cfo1,
				    group_cfo2 = EXCLUDED.group_cfo2, group_pl = EXCLUDED.group_pl,
				    expense = EXCLUDED.expense, legal_entity = EXCLUDED.legal_entity,
				    country_company = EXCLUDED.country_company, store_type = EXCLUDED.store_type,
				    reg_manager = EXCLUDED.reg_manager,
				    row_version = exp_row.row_version + 1, updated_at = NOW()`,
				instanceID, r.CodeCFO, r.CodePL, r.CFO, r.GroupCFO1, r.GroupCFO2,
				r.GroupPL, r.Expense, r.LegalEntity, r.CountryCompany, r.StoreType, r.RegManager)
		}
		br := s.pool.SendBatch(ctx, b)
		for range rows {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return 0, err
			}
		}
		if err := br.Close(); err != nil {
			return 0, err
		}
	}
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM exp_row WHERE instance_id = $1`, instanceID).Scan(&n)
	return n, err
}

// expWhere — условия отбора (ТЗ §7.1) и их аргументы.
func expWhere(instanceID int64, f ExpFilter) (string, []any) {
	w := []string{"instance_id = $1"}
	args := []any{instanceID}
	add := func(cond string, v any) {
		args = append(args, v)
		w = append(w, fmt.Sprintf(cond, len(args)))
	}
	if f.GroupPL != "" {
		add("group_pl = $%d", f.GroupPL)
	}
	if f.CodePL > 0 {
		add("code_pl = $%d", f.CodePL)
	}
	if f.CodeCFO > 0 {
		add("code_cfo = $%d", f.CodeCFO)
	}
	if f.City != "" {
		add("group_cfo2 = $%d", f.City)
	}
	if f.LegalEntity != "" {
		add("legal_entity = $%d", f.LegalEntity)
	}
	if f.StoreType != "" {
		add("store_type = $%d", f.StoreType)
	}
	if f.RegManager != "" {
		add("reg_manager = $%d", f.RegManager)
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		// Поиск ТЗ §7.1: статья, код PL, код ЦФО, наименование площадки/магазина.
		args = append(args, "%"+strings.ToLower(s)+"%")
		i := len(args)
		w = append(w, fmt.Sprintf(
			"(LOWER(expense) LIKE $%d OR LOWER(cfo) LIKE $%d OR code_pl::text LIKE $%d OR code_cfo::text LIKE $%d)",
			i, i, i, i))
	}
	return strings.Join(w, " AND "), args
}

// Rows — строки формы порцией плюс общее число строк отбора (ТЗ §7.10).
func (s *expRepo) Rows(ctx context.Context, instanceID int64, f ExpFilter) ([]ExpRow, int, error) {
	where, args := expWhere(instanceID, f)

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM exp_row WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	q := `SELECT id, code_cfo, code_pl, cfo, group_cfo1, group_cfo2, group_pl, expense,
	             legal_entity, country_company, store_type, reg_manager, comment
	        FROM exp_row WHERE ` + where + `
	       ORDER BY ` + expOrderBy(f) + `
	       LIMIT ` + fmt.Sprint(limit) + ` OFFSET ` + fmt.Sprint(max(f.Offset, 0))
	rs, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rs.Close()

	rows := make([]ExpRow, 0, limit)
	byID := map[int64]int{}
	for rs.Next() {
		var r ExpRow
		if err := rs.Scan(&r.ID, &r.CodeCFO, &r.CodePL, &r.CFO, &r.GroupCFO1, &r.GroupCFO2,
			&r.GroupPL, &r.Expense, &r.LegalEntity, &r.CountryCompany, &r.StoreType,
			&r.RegManager, &r.Comment); err != nil {
			return nil, 0, err
		}
		r.Values = make([]ExpValue, 0, 24)
		byID[r.ID] = len(rows)
		rows = append(rows, r)
	}
	if err := rs.Err(); err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return rows, total, nil
	}

	// Значения отдельным запросом по id страницы: JOIN на 24 ячейки размножил бы
	// атрибуты строки в 24 раза, а страницы здесь большие.
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	vs, err := s.pool.Query(ctx, `
		SELECT row_id, scenario, month, amount, source, note
		  FROM exp_value WHERE row_id = ANY($1)
		 ORDER BY scenario, month`, ids)
	if err != nil {
		return nil, 0, err
	}
	defer vs.Close()
	for vs.Next() {
		var (
			rowID int64
			v     ExpValue
			amt   *float64
		)
		if err := vs.Scan(&rowID, &v.Scenario, &v.Month, &amt, &v.Source, &v.Note); err != nil {
			return nil, 0, err
		}
		v.Amount = amt
		if i, ok := byID[rowID]; ok {
			rows[i].Values = append(rows[i].Values, v)
		}
	}
	return rows, total, vs.Err()
}

// SaveValues — сохранение плановых ячеек с записью в журнал (ТЗ §1.2).
//
// Пустое значение (amount = null) СТИРАЕТ ячейку, а не пишет ноль: очистка —
// отдельное действие ТЗ §7.8 («пусто не равно нулю»), и без неё ошибочно
// введённое число нечем убрать.
func (s *expRepo) SaveValues(ctx context.Context, instanceID int64, cells []ExpCell, actorID int64, cardVersion int) (int, error) {
	if len(cells) == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	n := 0
	for _, c := range cells {
		if c.Month < 1 || c.Month > 12 {
			return 0, fmt.Errorf("месяц вне диапазона: %d", c.Month)
		}
		var rowID int64
		err := tx.QueryRow(ctx, `
			SELECT id FROM exp_row
			 WHERE instance_id = $1 AND code_cfo = $2 AND code_pl = $3`,
			instanceID, c.CodeCFO, c.CodePL).Scan(&rowID)
		if err != nil {
			return 0, fmt.Errorf("строка ЦФО %d / статья %d не найдена в форме", c.CodeCFO, c.CodePL)
		}
		var old *float64
		_ = tx.QueryRow(ctx, `
			SELECT amount FROM exp_value
			 WHERE row_id = $1 AND scenario = 'plan' AND month = $2`, rowID, c.Month).Scan(&old)

		src := c.Source
		if src == "" {
			src = ExpSourceManual
		}
		if c.Amount == nil {
			if _, err := tx.Exec(ctx, `
				DELETE FROM exp_value WHERE row_id = $1 AND scenario = 'plan' AND month = $2`,
				rowID, c.Month); err != nil {
				return 0, err
			}
		} else if _, err := tx.Exec(ctx, `
			INSERT INTO exp_value (row_id, scenario, month, amount, source, note, updated_by)
			VALUES ($1,'plan',$2,$3,$4,$5,$6)
			ON CONFLICT (row_id, scenario, month) DO UPDATE SET
			    amount = EXCLUDED.amount, source = EXCLUDED.source,
			    note = EXCLUDED.note, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
			rowID, c.Month, *c.Amount, src, c.Note, nullID(actorID)); err != nil {
			return 0, err
		}
		if !sameAmount(old, c.Amount) {
			if _, err := tx.Exec(ctx, `
				INSERT INTO exp_value_version (row_id, scenario, month, amount_old, amount_new, card_version, changed_by)
				VALUES ($1,'plan',$2,$3,$4,$5,$6)`,
				rowID, c.Month, old, c.Amount, cardVersion, nullID(actorID)); err != nil {
				return 0, err
			}
			n++
		}
	}
	return n, tx.Commit(ctx)
}

// SaveReadOnly — снимок нередактируемого сценария из источника: факт прошлого
// года и стратегия планового (ТЗ §2.5, ответ Н1). Журнал не ведём: это не ввод
// человека, а перезаписываемое отражение источника.
func (s *expRepo) SaveReadOnly(ctx context.Context, instanceID int64, scenario string, cells []ExpCell) error {
	if len(cells) == 0 {
		return nil
	}
	if !ExpScenarioReadOnly(scenario) {
		return fmt.Errorf("сценарий %q редактируется формой — перезаписывать его из источника нельзя", scenario)
	}
	b := &pgx.Batch{}
	for _, c := range cells {
		if c.Amount == nil || c.Month < 1 || c.Month > 12 {
			continue
		}
		b.Queue(`
			INSERT INTO exp_value (row_id, scenario, month, amount, source)
			SELECT r.id, $6, $3, $4, 'source'
			  FROM exp_row r
			 WHERE r.instance_id = $1 AND r.code_cfo = $2 AND r.code_pl = $5
			ON CONFLICT (row_id, scenario, month) DO UPDATE SET
			    amount = EXCLUDED.amount, updated_at = NOW()`,
			instanceID, c.CodeCFO, c.Month, *c.Amount, c.CodePL, scenario)
	}
	br := s.pool.SendBatch(ctx, b)
	defer br.Close()
	for i := 0; i < b.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// SaveComment — комментарий строки (ТЗ §7.8, обязателен при E-18).
func (s *expRepo) SaveComment(ctx context.Context, instanceID int64, codeCFO, codePL int, comment string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE exp_row SET comment = $4, updated_at = NOW()
		 WHERE instance_id = $1 AND code_cfo = $2 AND code_pl = $3`,
		instanceID, codeCFO, codePL, comment)
	return err
}

// GroupTotals — итоги плана по группам PL (ТЗ §7.6, свёрнутый вид).
func (s *expRepo) GroupTotals(ctx context.Context, instanceID int64) (map[string]float64, error) {
	rs, err := s.pool.Query(ctx, `
		SELECT r.group_pl, COALESCE(SUM(v.amount), 0)
		  FROM exp_row r
		  LEFT JOIN exp_value v ON v.row_id = r.id AND v.scenario = 'plan'
		 WHERE r.instance_id = $1
		 GROUP BY r.group_pl`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rs.Close()
	out := map[string]float64{}
	for rs.Next() {
		var g string
		var t float64
		if err := rs.Scan(&g, &t); err != nil {
			return nil, err
		}
		out[g] = t
	}
	return out, rs.Err()
}

// sameAmount — равны ли значения ячейки с учётом «пусто». Нужно, чтобы в журнал
// не сыпались записи «было пусто, стало пусто» при пересохранении формы.
func sameAmount(a, b *float64) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

// nullID — 0 превращаем в NULL: users.id = 0 не существует, а FK его отвергнет.
func nullID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

// GroupOrder — порядок групп PL из справочника dir_pl_group (ответ 36 от
// 10.09.2026: «порядок групп задать справочником, не алфавитом»).
//
// Пустой результат — не ошибка: сервис откатывается на перечень ТЗ §7.3,
// зашитый в код. Так форма открывается и на базе, где справочник ещё не залит.
func (s *expRepo) GroupOrder(ctx context.Context, formKind string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.payload_json->>'group_pl'
		  FROM plans_directory_row r
		  JOIN plans_directory d ON d.id = r.directory_id
		 WHERE d.code = 'dir_pl_group'
		   AND r.payload_json->>'form_kind' = $1
		 ORDER BY COALESCE((r.payload_json->>'sort_order')::int, 999)`, formKind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, 14)
	for rows.Next() {
		var g *string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		if g != nil && *g != "" {
			out = append(out, *g)
		}
	}
	return out, rows.Err()
}

// ExpenseNames — наименования статей справочника dir_pl_line по коду PL.
// Нужны валидации E-17 «наименование статьи совпадает со справочником».
// Справочник может быть не залит — тогда контроль просто не срабатывает.
func (s *expRepo) ExpenseNames(ctx context.Context) (map[int]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT (r.payload_json->>'code_pl')::int, COALESCE(r.payload_json->>'expense_name','')
		  FROM plans_directory_row r
		  JOIN plans_directory d ON d.id = r.directory_id
		 WHERE d.code = 'dir_pl_line'
		   AND r.payload_json->>'code_pl' ~ '^[0-9]+$'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var code int
		var name string
		if err := rows.Scan(&code, &name); err != nil {
			return nil, err
		}
		if name != "" {
			out[code] = name
		}
	}
	return out, rows.Err()
}

// expOrderBy — ORDER BY по запрошенной колонке (ТЗ §7.3).
//
// Имя колонки приходит от клиента, поэтому подставляется ТОЛЬКО из белого списка
// expSortColumns; месяц — числом после проверки диапазона. Собирать ORDER BY из
// внешней строки нельзя даже «внутри админки».
//
// Порядок по умолчанию всегда добавляется вторым ключом: без него строки с
// одинаковым значением сортируемой колонки перескакивали бы между страницами, и
// подгрузка порциями показывала бы одни и те же строки дважды.
func expOrderBy(f ExpFilter) string {
	const fallback = "group_pl, code_cfo, code_pl"
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	if col, ok := expSortColumns[f.Sort]; ok {
		return col + " " + dir + " NULLS LAST, " + fallback
	}
	if m, ok := ExpSortMonth(f.Sort); ok {
		// Незаполненные месяцы уходят в конец при любом направлении: ТЗ §7.3
		// прямо говорит, что сортировкой по месяцу ищут «незаполненное или
		// подозрительное значение», и пустые не должны тонуть в середине.
		return fmt.Sprintf(
			"(SELECT v.amount FROM exp_value v WHERE v.row_id = exp_row.id AND v.scenario = 'plan' AND v.month = %d) %s NULLS LAST, %s",
			m, dir, fallback)
	}
	return fallback
}

// PnlGroups — второй уровень справочника статей: «код PL → укрупнённая группа
// P&L» (ответ 36, поле pnl_group в dir_pl_line). Пустое поле — не ошибка: такой
// код уходит в свод в группу «Не распределено» с подсветкой.
func (s *expRepo) PnlGroups(ctx context.Context) (map[int]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT (r.payload_json->>'code_pl')::int, COALESCE(r.payload_json->>'pnl_group','')
		  FROM plans_directory_row r
		  JOIN plans_directory d ON d.id = r.directory_id
		 WHERE d.code = 'dir_pl_line'
		   AND r.payload_json->>'code_pl' ~ '^[0-9]+$'
		   AND COALESCE(r.payload_json->>'pnl_group','') <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]string{}
	for rows.Next() {
		var code int
		var g string
		if err := rows.Scan(&code, &g); err != nil {
			return nil, err
		}
		out[code] = g
	}
	return out, rows.Err()
}

// ExpParamRow — параметр периода (ТЗ §6.1).
type ExpParamRow struct {
	ParamCode  string  `json:"param_code"`
	ScopeKind  string  `json:"scope_kind"`
	ScopeValue string  `json:"scope_value"`
	Value      float64 `json:"value"`
}

// Params — параметры периода формы.
func (s *expRepo) Params(ctx context.Context, instanceID int64) ([]ExpParamRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT param_code, scope_kind, scope_value, value
		  FROM exp_param WHERE instance_id = $1
		 ORDER BY param_code, scope_kind, scope_value`, instanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ExpParamRow, 0, 8)
	for rows.Next() {
		var p ExpParamRow
		if err := rows.Scan(&p.ParamCode, &p.ScopeKind, &p.ScopeValue, &p.Value); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SaveParam — параметр периода с записью в журнал (ТЗ §6.1: «кто и когда изменил
// параметр — сохраняется»).
func (s *expRepo) SaveParam(ctx context.Context, instanceID int64, p ExpParamRow, actorID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var old *float64
	_ = tx.QueryRow(ctx, `
		SELECT value FROM exp_param
		 WHERE instance_id = $1 AND param_code = $2 AND scope_kind = $3 AND scope_value = $4`,
		instanceID, p.ParamCode, p.ScopeKind, p.ScopeValue).Scan(&old)

	if _, err := tx.Exec(ctx, `
		INSERT INTO exp_param (instance_id, param_code, scope_kind, scope_value, value, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (instance_id, param_code, scope_kind, scope_value) DO UPDATE SET
		    value = EXCLUDED.value, updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
		instanceID, p.ParamCode, p.ScopeKind, p.ScopeValue, p.Value, nullID(actorID)); err != nil {
		return err
	}
	if old == nil || *old != p.Value {
		if _, err := tx.Exec(ctx, `
			INSERT INTO exp_param_version (instance_id, param_code, scope_kind, scope_value, value_old, value_new, changed_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			instanceID, p.ParamCode, p.ScopeKind, p.ScopeValue, old, p.Value, nullID(actorID)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SyncPLLines — пополнение справочника статей выгрузкой из источника (ответ И-12
// от 11.09.2026: «формировать справочник выгрузкой из источника, а не перечнем в
// ТЗ»). Перечень ТЗ оказался неполон: в файлах встречаются коды 10, 16, 17, 19,
// 33, 38, 57, 63, 84, 90, 92, 94, 99, 100, которых в нём нет.
//
// Добавляются ТОЛЬКО отсутствующие коды. Существующие строки не трогаем: в них
// живёт второй уровень (pnl_group) — решение НСИ, а не свойство источника, и
// перезапись стёрла бы его при каждом открытии формы.
func (s *expRepo) SyncPLLines(ctx context.Context, lines []ExpPLLine) (int, error) {
	if len(lines) == 0 {
		return 0, nil
	}
	var dirID int64
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO plans_directory (code, source, sync_status)
		VALUES ('dir_pl_line', 'olap', 'ok')
		ON CONFLICT (code) DO UPDATE SET sync_status = 'ok', synced_at = NOW()
		RETURNING id`).Scan(&dirID); err != nil {
		return 0, err
	}
	added := 0
	for _, l := range lines {
		if l.CodePL <= 0 {
			continue
		}
		ct, err := s.pool.Exec(ctx, `
			INSERT INTO plans_directory_row (directory_id, external_id, payload_json)
			SELECT $1, $2, jsonb_build_object(
			           'code_pl', $3::int, 'expense_name', $4::text, 'group_pl', $5::text)
			 WHERE NOT EXISTS (
			        SELECT 1 FROM plans_directory_row r
			         WHERE r.directory_id = $1 AND r.payload_json->>'code_pl' = $3::text)`,
			dirID, fmt.Sprint(l.CodePL), l.CodePL, l.Expense, l.GroupPL)
		if err != nil {
			return added, err
		}
		added += int(ct.RowsAffected())
	}
	return added, nil
}

// expAllRows — все строки экземпляра: Rows отдаёт не больше 5000 за раз (иначе 500), а строк
// у розницы BY больше (проверено живой выгрузкой — 5 716), поэтому читаем страницами.
func expAllRows(ctx context.Context, repo ExpRepo, instanceID int64) ([]ExpRow, error) {
	const page = 5000
	var all []ExpRow
	for off := 0; ; off += page {
		rows, total, err := repo.Rows(ctx, instanceID, ExpFilter{Limit: page, Offset: off})
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
		if len(rows) == 0 || off+len(rows) >= total {
			return all, nil
		}
	}
}

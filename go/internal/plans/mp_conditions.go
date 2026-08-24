package plans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Реестр «Условия площадки» (ТЗ МП §6.1) — ключевой новый объект скорректированного
// ТЗ. Одна запись = (площадка, период): %СПП, две наценки, доли всех статей прямых
// затрат, ставка НДС. После инверсии расчёта это ИСТОЧНИК ИСТИНЫ для расходной
// части: суммы формы — производные от условий.
//
// Требования ТЗ, которые здесь реализованы:
//   - условия копируются из предыдущего периода ОДНИМ действием и правятся точечно;
//   - согласующий видит, какие доли изменились и на сколько (diff);
//   - при отклонении сверх порога обязательно обоснование (МП-W1/W2);
//   - история изменений доступна (версия + автор + причина).

// MpConditions — условия площадки на период.
type MpConditions struct {
	ID             int64              `json:"id"`
	PlID           int64              `json:"pl_id"`
	CodeCFO        int                `json:"code_cfo"`
	NameCFO        string             `json:"name_cfo"`
	Segment        string             `json:"segment"`
	Country        string             `json:"country"`
	LegalEntity    string             `json:"legal_entity"`
	Year           int                `json:"period_year"`
	Month          int                `json:"period_month"`
	SppPct         float64            `json:"spp_pct"`
	MarkupPct      float64            `json:"markup_pct"`
	MarkupTotalPct float64            `json:"markup_total_pct"`
	VatRate        *float64           `json:"vat_rate,omitempty"` // nil → из справочника dir_vat
	Currency       string             `json:"currency"`
	Shares         map[string]float64 `json:"shares"` // block_type → доля статьи
	Version        int                `json:"version"`
	ChangeReason   string             `json:"change_reason"`
	Status         string             `json:"status"`
	AuthorID       int64              `json:"author_id"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

// MpConditionsDiff — изменение одной величины условий (для согласующего).
type MpConditionsDiff struct {
	CodeCFO   int     `json:"code_cfo"`
	NameCFO   string  `json:"name_cfo"`
	Field     string  `json:"field"`
	FieldName string  `json:"field_name"`
	Was       float64 `json:"was"`
	Now       float64 `json:"now"`
	Delta     float64 `json:"delta"`
	NeedsWhy  bool    `json:"needs_why"` // отклонение сверх порога → обязательно обоснование
}

// MpConditionsStore — хранилище условий и общих затрат.
type MpConditionsStore interface {
	Conditions(ctx context.Context, plID int64, year, month int) ([]MpConditions, error)
	SaveConditions(ctx context.Context, c MpConditions) (MpConditions, error)
	// CopyConditions — заполнить период условиями предыдущего (ТЗ §6.1).
	CopyConditions(ctx context.Context, fromPlID, toPlID int64, fromY, fromM, toY, toM int, allowed []int) (int, error)
	CommonCosts(ctx context.Context, plID int64, segment string, year, month int) ([]MpCommonCost, error)
	SaveCommonCosts(ctx context.Context, plID int64, rows []MpCommonCost, actorID int64) error
	// CalcOwner — владелец расчёта пары (статья, ЦФО): ТЗ §8.1, контроль МП-08.
	CalcOwner(ctx context.Context, codePL, codeCFO int) (string, bool, error)
	LogCalc(ctx context.Context, plID int64, codeCFO, year, month int, mode string, version int, cells []MpInverseCell) error
}

// MpCommonCost — общие затраты по МП (кроме прямых) по статье PL.
type MpCommonCost struct {
	Segment  string  `json:"segment"`
	CodePL   int     `json:"code_pl"`
	Name     string  `json:"name"`
	Group    string  `json:"group"`
	Year     int     `json:"period_year"`
	Month    int     `json:"period_month"`
	Currency string  `json:"currency"`
	Amount   float64 `json:"amount"`
	Source   string  `json:"source"`
}

type pgMpConditions struct{ pool *pgxpool.Pool }

// NewMpConditionsStore — конструктор.
func NewMpConditionsStore(pool *pgxpool.Pool) MpConditionsStore { return &pgMpConditions{pool: pool} }

func (s *pgMpConditions) Conditions(ctx context.Context, plID int64, year, month int) ([]MpConditions, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.pl_id, c.code_cfo, c.period_year, c.period_month, c.spp_pct,
		       c.markup_pct, c.markup_total_pct, c.vat_rate, c.currency, c.version,
		       c.change_reason, c.status, COALESCE(c.author_id,0), c.updated_at
		  FROM mp_conditions c
		 WHERE c.pl_id = $1 AND c.period_year = $2 AND c.period_month = $3
		 ORDER BY c.code_cfo`, plID, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MpConditions, 0)
	byID := map[int64]int{}
	for rows.Next() {
		var c MpConditions
		if err := rows.Scan(&c.ID, &c.PlID, &c.CodeCFO, &c.Year, &c.Month, &c.SppPct,
			&c.MarkupPct, &c.MarkupTotalPct, &c.VatRate, &c.Currency, &c.Version,
			&c.ChangeReason, &c.Status, &c.AuthorID, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.Shares = map[string]float64{}
		byID[c.ID] = len(out)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	// Доли статей — отдельной выборкой (набор статей у площадок разный).
	itemRows, err := s.pool.Query(ctx, `
		SELECT i.conditions_id, i.block_type, i.share_pct
		  FROM mp_condition_item i
		  JOIN mp_conditions c ON c.id = i.conditions_id
		 WHERE c.pl_id = $1 AND c.period_year = $2 AND c.period_month = $3`, plID, year, month)
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var id int64
		var block string
		var share float64
		if err := itemRows.Scan(&id, &block, &share); err != nil {
			return nil, err
		}
		if idx, ok := byID[id]; ok {
			out[idx].Shares[block] = share
		}
	}
	// Атрибуты площадки — из справочника.
	attrs := map[int]MarketplaceRow{}
	for _, mp := range MarketplaceSeed() {
		attrs[mp.CodeCFO] = mp
	}
	for i := range out {
		mp := attrs[out[i].CodeCFO]
		out[i].NameCFO, out[i].Segment = mp.NameCFO, mp.Segment
		out[i].Country, out[i].LegalEntity = mp.Country, mp.LegalEntity
	}
	return out, itemRows.Err()
}

func (s *pgMpConditions) SaveConditions(ctx context.Context, c MpConditions) (MpConditions, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return MpConditions{}, err
	}
	defer tx.Rollback(ctx)

	var author any
	if c.AuthorID != 0 {
		author = c.AuthorID
	}
	var id int64
	var version int
	err = tx.QueryRow(ctx, `
		INSERT INTO mp_conditions (pl_id, code_cfo, period_year, period_month, spp_pct,
			markup_pct, markup_total_pct, vat_rate, currency, change_reason, status, author_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,COALESCE(NULLIF($11,''),'draft'),$12)
		ON CONFLICT (pl_id, code_cfo, period_year, period_month) DO UPDATE SET
			spp_pct = EXCLUDED.spp_pct,
			markup_pct = EXCLUDED.markup_pct,
			markup_total_pct = EXCLUDED.markup_total_pct,
			vat_rate = EXCLUDED.vat_rate,
			currency = EXCLUDED.currency,
			change_reason = EXCLUDED.change_reason,
			status = EXCLUDED.status,
			author_id = EXCLUDED.author_id,
			version = mp_conditions.version + 1,
			updated_at = NOW()
		RETURNING id, version`,
		c.PlID, c.CodeCFO, c.Year, c.Month, c.SppPct, c.MarkupPct, c.MarkupTotalPct,
		c.VatRate, c.Currency, c.ChangeReason, c.Status, author).Scan(&id, &version)
	if err != nil {
		return MpConditions{}, err
	}
	// Доли перезаписываем целиком: набор статей у площадки может измениться.
	if _, err := tx.Exec(ctx, `DELETE FROM mp_condition_item WHERE conditions_id = $1`, id); err != nil {
		return MpConditions{}, err
	}
	plOf := blockToPL()
	for block, share := range c.Shares {
		if _, err := tx.Exec(ctx, `
			INSERT INTO mp_condition_item (conditions_id, block_type, code_pl, share_pct)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (conditions_id, block_type) DO UPDATE SET share_pct = EXCLUDED.share_pct`,
			id, block, plOf[block], share); err != nil {
			return MpConditions{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return MpConditions{}, err
	}
	c.ID, c.Version = id, version
	return c, nil
}

// CopyConditions — «заполнить период условиями предыдущего» одним действием.
func (s *pgMpConditions) CopyConditions(ctx context.Context, fromPlID, toPlID int64, fromY, fromM, toY, toM int, allowed []int) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// Копируем шапки условий, затем доли: INSERT … SELECT с переносом периода.
	rows, err := tx.Query(ctx, `
		WITH src AS (
			SELECT * FROM mp_conditions
			 WHERE pl_id = $1 AND period_year = $2 AND period_month = $3
			   AND ($6::int[] IS NULL OR code_cfo = ANY($6))
		), ins AS (
			INSERT INTO mp_conditions (pl_id, code_cfo, period_year, period_month, spp_pct,
				markup_pct, markup_total_pct, vat_rate, currency, change_reason, status, author_id)
			SELECT $4, code_cfo, $5, $7, spp_pct, markup_pct, markup_total_pct, vat_rate,
			       currency, 'скопировано из предыдущего периода', 'draft', author_id
			  FROM src
			ON CONFLICT (pl_id, code_cfo, period_year, period_month) DO UPDATE SET
				spp_pct = EXCLUDED.spp_pct, markup_pct = EXCLUDED.markup_pct,
				markup_total_pct = EXCLUDED.markup_total_pct, vat_rate = EXCLUDED.vat_rate,
				currency = EXCLUDED.currency, updated_at = NOW()
			RETURNING id, code_cfo
		)
		SELECT ins.id, src.id FROM ins JOIN src ON src.code_cfo = ins.code_cfo`,
		fromPlID, fromY, fromM, toPlID, toY, allowedArray(allowed), toM)
	if err != nil {
		return 0, err
	}
	type pair struct{ dst, src int64 }
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.dst, &p.src); err != nil {
			rows.Close()
			return 0, err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, p := range pairs {
		if _, err := tx.Exec(ctx, `DELETE FROM mp_condition_item WHERE conditions_id = $1`, p.dst); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO mp_condition_item (conditions_id, block_type, code_pl, share_pct)
			SELECT $1, block_type, code_pl, share_pct FROM mp_condition_item WHERE conditions_id = $2`,
			p.dst, p.src); err != nil {
			return 0, err
		}
	}
	return len(pairs), tx.Commit(ctx)
}

// allowedArray — nil для «без ограничения» (админ), иначе массив разрешённых ЦФО.
func allowedArray(allowed []int) any {
	if len(allowed) == 0 {
		return nil
	}
	return allowed
}

func (s *pgMpConditions) CommonCosts(ctx context.Context, plID int64, segment string, year, month int) ([]MpCommonCost, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT segment, code_pl, period_year, period_month, currency, amount, source
		  FROM mp_common_cost
		 WHERE pl_id = $1 AND ($2 = '' OR segment = $2 OR segment = '')
		   AND period_year = $3 AND period_month = $4
		 ORDER BY code_pl`, plID, segment, year, month)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := plLineNames()
	groups := plLineGroups()
	out := make([]MpCommonCost, 0)
	for rows.Next() {
		var c MpCommonCost
		if err := rows.Scan(&c.Segment, &c.CodePL, &c.Year, &c.Month, &c.Currency, &c.Amount, &c.Source); err != nil {
			return nil, err
		}
		c.Name, c.Group = names[c.CodePL], groups[c.CodePL]
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *pgMpConditions) SaveCommonCosts(ctx context.Context, plID int64, rows []MpCommonCost, actorID int64) error {
	if len(rows) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var actor any
	if actorID != 0 {
		actor = actorID
	}
	for _, r := range rows {
		if r.Currency == "" {
			r.Currency = "RUB"
		}
		if r.Source == "" {
			r.Source = "manual"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO mp_common_cost (pl_id, segment, code_pl, period_year, period_month,
				currency, amount, source, updated_by, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NOW())
			ON CONFLICT (pl_id, segment, code_pl, period_year, period_month, currency)
			DO UPDATE SET amount = EXCLUDED.amount, source = EXCLUDED.source,
				updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
			plID, r.Segment, r.CodePL, r.Year, r.Month, r.Currency, r.Amount, r.Source, actor); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *pgMpConditions) CalcOwner(ctx context.Context, codePL, codeCFO int) (string, bool, error) {
	var form string
	err := s.pool.QueryRow(ctx, `
		SELECT owner_form FROM plans_calc_owner
		 WHERE code_pl = $1 AND code_cfo = $2
		   AND valid_from <= CURRENT_DATE AND (valid_to IS NULL OR valid_to >= CURRENT_DATE)
		 ORDER BY valid_from DESC LIMIT 1`, codePL, codeCFO).Scan(&form)
	if err != nil {
		return "", false, nil // владелец не задан — контроль двойного счёта не применяется
	}
	return form, true, nil
}

func (s *pgMpConditions) LogCalc(ctx context.Context, plID int64, codeCFO, year, month int, mode string, version int, cells []MpInverseCell) error {
	if len(cells) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, c := range cells {
		if _, err := tx.Exec(ctx, `
			INSERT INTO mp_calc_log (pl_id, code_cfo, block_type, period_year, period_month,
				formula, inputs, result, conditions_version, calc_mode)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			plID, codeCFO, c.BlockType, year, month, c.Formula,
			mustJSON(map[string]any{"calculated": c.Calculated, "manual": c.Manual}),
			c.Value, version, mode); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// ---------- Чистая логика (тестируется без БД) ----------

// MpDiffThresholds — пороги, сверх которых нужно обоснование (ТЗ §6.1, МП-W1/W2).
type MpDiffThresholds struct {
	SppPct    float64 // изменение %СПП в абсолютных пунктах
	MarkupPct float64
	SharePct  float64
}

// DefaultMpThresholds — стартовые пороги: 2 п.п. по СПП и наценке, 1 п.п. по доле
// статьи. Значения настраиваемые; выбраны как «заметное изменение переговорных
// условий», требующее пояснения согласующему.
func DefaultMpThresholds() MpDiffThresholds {
	return MpDiffThresholds{SppPct: 0.02, MarkupPct: 0.02, SharePct: 0.01}
}

// diffConditions — что изменилось между периодами. Согласующий видит, какие доли
// изменились и на сколько (ТЗ §6.1); превышение порога помечается NeedsWhy.
func diffConditions(prev, next []MpConditions, th MpDiffThresholds) []MpConditionsDiff {
	prevByCfo := map[int]MpConditions{}
	for _, c := range prev {
		prevByCfo[c.CodeCFO] = c
	}
	names := mpLineByBlock()
	var out []MpConditionsDiff
	for _, c := range next {
		p, had := prevByCfo[c.CodeCFO]
		add := func(field, fieldName string, was, now, limit float64) {
			if !had {
				was = 0
			}
			if math.Abs(now-was) < 1e-9 {
				return
			}
			out = append(out, MpConditionsDiff{
				CodeCFO: c.CodeCFO, NameCFO: c.NameCFO, Field: field, FieldName: fieldName,
				Was: was, Now: now, Delta: now - was,
				NeedsWhy: math.Abs(now-was) > limit,
			})
		}
		add("spp_pct", "% СПП", p.SppPct, c.SppPct, th.SppPct)
		add("markup_pct", "Наценка, %", p.MarkupPct, c.MarkupPct, th.MarkupPct)
		add("markup_total_pct", "Наценка от общей сс, %", p.MarkupTotalPct, c.MarkupTotalPct, th.MarkupPct)
		// Доли статей: объединяем наборы, чтобы увидеть и появившиеся, и исчезнувшие.
		blocks := map[string]bool{}
		for b := range c.Shares {
			blocks[b] = true
		}
		for b := range p.Shares {
			blocks[b] = true
		}
		keys := make([]string, 0, len(blocks))
		for b := range blocks {
			keys = append(keys, b)
		}
		sort.Strings(keys)
		for _, b := range keys {
			name := b
			if l, ok := names[b]; ok {
				name = l.Name
			}
			add("share:"+b, "Доля: "+name, p.Shares[b], c.Shares[b], th.SharePct)
		}
	}
	return out
}

// requireReasonFor — перечень изменений, по которым обязательно обоснование.
func requireReasonFor(diffs []MpConditionsDiff) []MpConditionsDiff {
	var out []MpConditionsDiff
	for _, d := range diffs {
		if d.NeedsWhy {
			out = append(out, d)
		}
	}
	return out
}

// validateConditions — проверки условий площадки перед сохранением.
// МП-02: %СПП, наценки и доли в допустимых диапазонах (доля может быть
// отрицательной только для компенсаций — статьи 66).
// МП-03: сумма долей прямых затрат не превышает 100 % выручки по ценам менеджера.
func validateConditions(c MpConditions) error {
	if c.CodeCFO == 0 {
		return errors.New("не указана площадка")
	}
	if c.SppPct < 0 || c.SppPct >= 1 {
		return fmt.Errorf("%%СПП должен быть в диапазоне [0, 100): получено %.2f%%", c.SppPct*100)
	}
	if c.MarkupPct <= -1 {
		return errors.New("наценка не может быть меньше −100 %")
	}
	if c.MarkupTotalPct <= -1 {
		return errors.New("наценка от общей себестоимости не может быть меньше −100 %")
	}
	var total float64
	for block, share := range c.Shares {
		if share < 0 && !mpCompensationBlock(block) {
			return fmt.Errorf("отрицательная доля допустима только для компенсаций, а не для «%s»", mpBlockName(block))
		}
		if share > 1 {
			return fmt.Errorf("доля статьи «%s» больше 100 %% выручки", mpBlockName(block))
		}
		total += share
	}
	// Комиссия (=%СПП) входит в прямые затраты, поэтому в контроль 100 % тоже.
	if total+c.SppPct > 1 {
		return fmt.Errorf("сумма долей прямых затрат (%.2f %%) вместе с %%СПП (%.2f %%) превышает 100 %% выручки",
			total*100, c.SppPct*100)
	}
	return nil
}

// mpCompensationBlock — статьи, где отрицательная доля осмысленна: «прочие
// удержания и компенсации» и штрафы (возвраты штрафов) — оба CodePL 66.
func mpCompensationBlock(block string) bool {
	return block == BCostOther || block == BCostPenalties
}

func mpBlockName(block string) string {
	if l, ok := mpLineByBlock()[block]; ok {
		return l.Name
	}
	return block
}

// blockToPL — block_type → code_pl (обратная plToBlock, для записи долей).
func blockToPL() map[string]int {
	out := map[string]int{}
	for _, l := range mpFormSpec() {
		if l.CodePL > 0 {
			out[l.BlockType] = l.CodePL
		}
	}
	out[BCostPenalties] = mpPenaltyPL
	return out
}

// conditionsToInput — условия площадки → вход каскада. Ставка НДС: из условий,
// иначе из справочника (ТЗ §3.4 — прошивать нельзя).
func conditionsToInput(c MpConditions, vat float64) MpConditionsInput {
	rate := vat
	if c.VatRate != nil {
		rate = *c.VatRate
	}
	shares := make(map[string]float64, len(c.Shares))
	for k, v := range c.Shares {
		shares[k] = v
	}
	return MpConditionsInput{
		CodeCFO: c.CodeCFO, VatRate: rate, SppPct: c.SppPct,
		MarkupPct: c.MarkupPct, MarkupTotalPct: c.MarkupTotalPct, Shares: shares,
	}
}

// plLineNames / plLineGroups — названия и группы статей PL из справочника.
func plLineNames() map[int]string {
	out := map[int]string{}
	for _, l := range PLLineSeed() {
		out[l.CodePL] = l.Name
	}
	return out
}

func plLineGroups() map[int]string {
	out := map[int]string{}
	for code, group := range mpCommonCostGroups() {
		for _, pl := range group {
			out[pl] = code
		}
	}
	return out
}

// mpCommonCostGroups — 7 групп статей общих затрат по МП (ТЗ §4.3).
// Эти статьи НЕ считаются от продаж: вводятся или копируются из предыдущего
// периода / стратегии.
func mpCommonCostGroups() map[string][]int {
	return map[string][]int{
		"ВЫПЛАТЫ РАБОТНИКАМ":               {24, 26, 27, 28, 29, 30, 31, 32, 78, 79, 80, 81, 86, 87, 88, 89},
		"РЕКЛАМА И МАРКЕТИНГ":              {10, 12, 13, 14, 15, 16, 17, 18, 19, 22, 23, 91},
		"ПРОЧИЕ РАСХОДЫ (в т.ч. комиссия)": {58, 60, 61, 62, 63, 64, 65, 66, 83},
		"ОСНОВНЫЕ РАСХОДЫ":                 {25, 33, 35, 36, 37, 38, 39, 40, 41, 42, 45, 84, 92},
		"ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ":          {21, 46, 47, 48, 49, 50, 51, 52, 53, 85, 93},
		"АРЕНДА":      {54, 55},
		"АМОРТИЗАЦИЯ": {67},
	}
}

// MpCommonCostGroup — группа статей общих затрат для UI.
type MpCommonCostGroup struct {
	Group string      `json:"group"`
	Lines []PLLineRow `json:"lines"`
}

// MpCommonCostSpec — спека блока «Общие затраты по МП» (7 групп, ТЗ §4.3).
func MpCommonCostSpec() []MpCommonCostGroup {
	names := plLineNames()
	groups := mpCommonCostGroups()
	order := []string{
		"ВЫПЛАТЫ РАБОТНИКАМ", "РЕКЛАМА И МАРКЕТИНГ", "ПРОЧИЕ РАСХОДЫ (в т.ч. комиссия)",
		"ОСНОВНЫЕ РАСХОДЫ", "ВСПОМОГАТЕЛЬНЫЕ РАСХОДЫ", "АРЕНДА", "АМОРТИЗАЦИЯ",
	}
	out := make([]MpCommonCostGroup, 0, len(order))
	for _, g := range order {
		lines := make([]PLLineRow, 0, len(groups[g]))
		for _, pl := range groups[g] {
			name := names[pl]
			if name == "" {
				name = "Статья " + itoa(int64(pl))
			}
			lines = append(lines, PLLineRow{CodePL: pl, Name: name, GroupPL: g})
		}
		out = append(out, MpCommonCostGroup{Group: g, Lines: lines})
	}
	return out
}

// mustJSON — сериализация входов для лога расчёта.
func mustJSON(v map[string]any) []byte {
	raw, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}

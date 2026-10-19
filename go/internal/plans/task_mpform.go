package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
)

// Форма ввода МП — TASK-DRIVEN и ПОЛНАЯ (весь каскад как в прототипе large):
// площадки = ЦФО задания, строки = mpFormSpec() (продажи→СПП→наценка→маржа→COGS→
// прямые затраты→PL). Ввод — 1046 + %СПП + себестоимость + статьи затрат; всё
// производное считает computeMpPlatform (сервер) и зеркалит клиент (живой пересчёт).
// Факт/стратегия — read-only. Права: assignee/delegate/admin.

// MpFormPlatform — площадка МП (ЦФО задания).
type MpFormPlatform struct {
	CodeCFO     int    `json:"code_cfo"`
	Name        string `json:"name"`
	Country     string `json:"country"`
	Segment     string `json:"segment"`      // large|small — группировка колонок в объединённой форме
	LegalEntity string `json:"legal_entity"` // для фильтра «по ЮЛ» в форме и своде
}

// distinctSegments — набор сегментов, встречающихся среди площадок задания.
func distinctSegments(segmentOf map[int]string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 2)
	for _, seg := range segmentOf {
		if seg != "" && !seen[seg] {
			seen[seg] = true
			out = append(out, seg)
		}
	}
	sort.Strings(out)
	return out
}

// MpFormCell — ячейка (площадка × строка): вычисленное значение + ввод/факт/стратегия.
type MpFormCell struct {
	CodeCFO       int      `json:"code_cfo"`
	BlockType     string   `json:"block_type"`
	Value         float64  `json:"value"`                   // вычисленное значение каскада (отображение)
	Fact          *float64 `json:"fact"`                    // из источника (для input-строк)
	FactPrev      *float64 `json:"fact_prev"`               // факт того же месяца прошлого года (SPEC §9.6)
	FactPrevMonth *float64 `json:"fact_prev_month"`         // факт предыдущего месяца
	Strategy      *float64 `json:"strategy"`                // стратегический бюджет (read-only)
	Target        *float64 `json:"target"`                  // тактика-таргет (FormToLoaTaktTarget, read-only)
	Tactic        *float64 `json:"tactic"`                  // сохранённая тактика (editable); nil если нет
	IsManual      bool     `json:"is_manual"`               // ручная корректировка (ADJ-02)
	Reason        string   `json:"reason,omitempty"`        // причина корректировки
	MissingShare  bool     `json:"missing_share,omitempty"` // доля статьи не задана на дату тактики
}

// MpTaskForm — структура формы задания МП.
type MpTaskForm struct {
	Task     Task   `json:"task"`
	Segment  string `json:"segment"`
	Year     int    `json:"year"`
	Month    int    `json:"month"`
	Currency string `json:"currency"`
	// Vat — ставка для шапки. Осмысленна, только когда она одна на всю форму:
	// при разных ставках площадок (VatMixed) шапка обязана показать разрез, а не
	// одно число — иначе повторится дефект «в шапке 19,60 %, в расчёте 20 %».
	Vat float64 `json:"vat"`
	// VatByCfo — базис перевода «с НДС → без НДС» каждой площадки. Клиент считает
	// каскад по нему же: пока он делил на свою константу 0,20, форма показывала
	// одно, а сервер считал другое.
	VatByCfo map[int]MpVatBasis `json:"vat_by_cfo,omitempty"`
	// NormByCfo — норматив удержаний площадки на месяц формы (dir_mp_norm, сид —
	// фолбэк), в базе цены менеджера без НДС. Клиентский каскад приводит его к
	// базе долей и считает контроль норматива так же, как сервер: без этого поля
	// строки «Норматив удержаний» и «Контроль норматива» на экране были нулями.
	NormByCfo   map[int]float64 `json:"norm_by_cfo,omitempty"`
	VatMixed    bool            `json:"vat_mixed"`
	VatFromPlan bool               `json:"vat_from_plan"`
	// VatMissing — площадки, по которым плана нет ни за месяц формы, ни за
	// предыдущий: суммы посчитаны по аварийной ставке справочника, отправка на
	// согласование заблокирована (МП-12).
	VatMissing []int            `json:"vat_missing,omitempty"`
	Platforms  []MpFormPlatform `json:"platforms"`
	Lines      []MpLine         `json:"lines"`
	Cells      []MpFormCell     `json:"cells"`
	// CalcMode — направление расчёта карточки: legacy («суммы → доли») либо
	// inverse («условия → суммы», ТЗ §3.1). От него зависит, какие строки
	// доступны на ввод, поэтому клиент обязан его учитывать.
	CalcMode string `json:"calc_mode"`
	Editable bool   `json:"editable"`
	// PLWithCommon — в PL формы вычитаются общие затраты. На этапах 1.1/1.2 они
	// не участвуют, начиная с 1.5 — участвуют (правка 08.09.2026 п. 10).
	PLWithCommon bool `json:"pl_with_common"`
	// CardID — карточка процесса этой формы (0, если период старый и карточки нет).
	CardID int64 `json:"card_id,omitempty"`
	// Conditions — условия площадок, из которых посчитана расходная часть
	// (в inverse-режиме): %СПП, наценки и доли статей для показа рядом со строками.
	Conditions map[int]MpConditions `json:"conditions,omitempty"`
	// Hints — подсказки к полям ввода (правки ТЗ 02.09.2026, часть 1): фактическое
	// значение источника рядом с полем, БЕЗ подстановки в значение и без
	// блокировки. Сохранение их не касается.
	Hints []FieldHint `json:"hints,omitempty"`
	// FactSnapshot — отметка «данные актуальны на …» (правка МП §12). Форма читает
	// снимок, а не OLAP вживую, и обязана показывать, когда он собран: иначе
	// заполняющий не отличит свежий факт от факта месячной давности.
	FactSnapshot *MpSnapshotMeta `json:"fact_snapshot,omitempty"`
}

type mpVersionSnapshot struct {
	Conditions map[int]MpConditions `json:"conditions"`
	Values     []struct {
		CodeCFO   int     `json:"code_cfo"`
		BlockType string  `json:"block_type"`
		Amount    float64 `json:"amount"`
		Currency  string  `json:"currency"`
	} `json:"values"`
}

func (s *TaskStore) mpClosedSnapshot(ctx context.Context, card Card, currency string) (map[int]MpConditions, map[int]map[string]float64) {
	if card.ID == 0 || cardEditable(card) {
		return nil, nil
	}
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT payload FROM card_version WHERE card_id=$1 AND version_no <= $2 ORDER BY version_no DESC LIMIT 1`, card.ID, card.CurrentVersion).Scan(&raw); err != nil {
		return nil, nil
	}
	var snapshot mpVersionSnapshot
	if json.Unmarshal(raw, &snapshot) != nil {
		return nil, nil
	}
	values := map[int]map[string]float64{}
	lines := mpLineByBlock()
	for _, row := range snapshot.Values {
		if values[row.CodeCFO] == nil {
			values[row.CodeCFO] = map[string]float64{}
		}
		amount := row.Amount
		if lines[row.BlockType].ValueKind != ValuePct {
			amount = convertAmount(amount, row.Currency, currency)
		}
		values[row.CodeCFO][row.BlockType] = amount
	}
	return snapshot.Conditions, values
}

// seedPL — code_pl для подтяжки input-строк из источника (SUMIFS прототипа).
var seedPL = map[string]int{
	BSalesManagerGross: 1046,
	BShipments:         8006,
	BCostAgent:         64,
	BCostFreight:       51,
	BCostLogTransport:  52,
	BCostLogWarehouse:  54,
	BCostAds:           13,
	BCostAdsSocial:     15,
	BCostPackaging:     45,
	BCostAcquiring:     58,
	BCostIT:            48,
	BCostOther:         66,
}

// taskBrief читает задание (cfo_codes, pl_id, форма) + период/сегмент.
func (s *TaskStore) taskBrief(ctx context.Context, taskID int64) (Task, int, int, string, error) {
	var t Task
	var raw []byte
	err := s.pool.QueryRow(ctx, taskSelect+` WHERE t.id=$1`, taskID).Scan(
		&t.ID, &t.PlID, &t.StageCode, &t.FormCode, &t.Title, &raw, &t.Role,
		&t.PositionID, &t.LegalEntity, &t.AssigneeID, &t.AssigneeName,
		&t.DelegateID, &t.DelegateName, &t.Status,
		&t.DelegatedBy, &t.DelegatedName, &t.DelegatedAt, &t.DelegateNote, &t.DueAt)
	if err != nil {
		return t, 0, 0, "", err
	}
	_ = json.Unmarshal(raw, &t.CfoCodes)
	t.CfoCount = len(t.CfoCodes)
	var year, month int
	_ = s.pool.QueryRow(ctx, `SELECT period_year, period_month FROM pl_instance WHERE id=$1`, t.PlID).Scan(&year, &month)
	var segment string
	if len(t.CfoCodes) > 0 {
		// Сравниваем код КАК ТЕКСТ, а не приводим колонку к int: в реальном
		// справочнике ЦФО встречаются нечисловые коды (в проде на «40RUBK»
		// падал синк dir_retail_store). Приведение всей колонки роняет запрос
		// целиком — даже когда искомая строка числовая.
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(payload_json->>'segment','') FROM plans_directory_row r JOIN plans_directory d ON d.id=r.directory_id
			WHERE d.code='dir_marketplace' AND payload_json->>'code_cfo' = $1::text LIMIT 1`, t.CfoCodes[0]).Scan(&segment)
	}
	return t, year, month, segment, nil
}

// mpTotalsSegment — сегмент итоговых строк задания (скидки и уценки по всем
// площадкам): общий сегмент всех площадок задания. У задания на обе группы — ""
// (одна строка на всё задание). Сегмент из taskBrief для этого не годится: на
// стенде он приходил пустым (19.10.2026), и итоговые строки крупных и мелких МП
// попадали под один ключ pl_metric и затирали друг друга.
//
// Код ЦФО, которого нет в справочнике (segmentOf[c] == ""), сегмента не даёт: если он
// попал в задание вместе с известными, итог считается смешанным (""), а у задания
// только из таких кодов итог остаётся общим — как до разделения по сегментам.
func mpTotalsSegmentFor(scope string, segmentOf map[int]string, cfos []int) string {
	// Форма «Крупные МП» / «Мелкие МП» задания на обе группы — свои итоги: иначе обе
	// карточки писали бы одну строку скидок, а закрытость карточки не проверялась.
	// Разрез приходит от клиента: принимаем его, только если у задания есть площадки
	// этого сегмента. Иначе итог попал бы под чужой сегмент того же pl_id, мимо блокировки
	// и проверки закрытости карточки (SaveMpForm отвергает такой разрез ошибкой раньше).
	if scope == "large" || scope == "small" {
		for _, c := range cfos {
			if segmentOf[c] == scope {
				return scope
			}
		}
	}
	return mpTotalsSegment(segmentOf, cfos)
}

func mpTotalsSegment(segmentOf map[int]string, cfos []int) string {
	seg := ""
	for i, c := range cfos {
		s := segmentOf[c]
		if i == 0 {
			seg = s
			continue
		}
		if s != seg {
			return ""
		}
	}
	return seg
}

// factSeed — сумма из источника для input-строки (block) по площадке (cfo).
func factSeed(factIdx map[[2]int]float64, block string, cfo int) (float64, bool) {
	switch block {
	case BCogsTotal:
		a, ok1 := factIdx[[2]int{cfo, 2006}]
		b, ok2 := factIdx[[2]int{cfo, 6006}]
		if !ok1 && !ok2 {
			return 0, false
		}
		return a + b, true
	case BCostPenalties:
		v, ok := factIdx[[2]int{cfo, mpPenaltyFactKey}] // штрафы FINDWH (FINDWHACCESSGROUP)
		return v, ok
	default:
		pl, has := seedPL[block]
		if !has {
			return 0, false
		}
		v, ok := factIdx[[2]int{cfo, pl}]
		return v, ok
	}
}

// deriveSPP — фактический %СПП, посчитанный из факта (пара 1006/1045 либо
// 1022/1046). Это ПОДСКАЗКА, а не начальное значение поля: правки ТЗ 02.09.2026
// (часть 1, МП §8) требуют, чтобы %СПП вводился руками, потому что в форме Excel
// за ним нет формулы. Подставлять источник нельзя — через одно сохранение
// подстановка становится неотличимой от решения планировщика.
func deriveSPP(factIdx map[[2]int]float64, cfo int) float64 {
	if net, ok := factIdx[[2]int{cfo, 1006}]; ok {
		if mgr, ok2 := factIdx[[2]int{cfo, 1045}]; ok2 && mgr != 0 {
			return 1 - net/mgr
		}
	}
	if g, ok := factIdx[[2]int{cfo, 1022}]; ok {
		if mgr, ok2 := factIdx[[2]int{cfo, 1046}]; ok2 && mgr != 0 {
			return 1 - g/mgr
		}
	}
	return 0
}

// MpFormData собирает полную форму МП задания (каскад по спеке) в валюте currency
// (BYN/RUB/USD; пусто → RUB). Тактика хранится в RUB, пересчёт — только отображение.
// scope — разрез КАРТОЧКИ формы (large|small|пусто). Крупные и мелкие МП живут
// раздельными карточками со своими маршрутами и согласующими, но задание может
// покрывать обе группы. Без разреза обе карточки открывали одну и ту же форму:
// на экране это выглядело так, что крупные можно согласовать, а мелкие — только
// отправить (замечание аналитика 08.09.2026).
func (s *TaskStore) MpFormData(ctx context.Context, taskID int64, currency, scope string) (MpTaskForm, error) {
	var f MpTaskForm
	t, year, month, segment, err := s.taskBrief(ctx, taskID)
	if err != nil {
		return f, err
	}
	if scope == "large" || scope == "small" {
		segment = scope
	}
	f.Task, f.Year, f.Month, f.Segment, f.Currency = t, year, month, segment, normalizeCurrency(currency)

	// Режим расчёта карточки определяет и состав вводимых строк, и способ
	// получения расходной части (ТЗ §3.1).
	plID := t.PlID
	mode, card := s.calcModeFor(ctx, plID, segment)
	f.CalcMode, f.CardID = mode, card.ID
	f.Editable = card.ID == 0 || cardEditable(card)
	f.PLWithCommon = mpPLWithCommon(card)
	f.Lines = mpFormSpecFor(mode)

	// Условия площадок нужны и для расчёта, и для показа долей в сетке.
	condByCfo := map[int]MpConditions{}
	if s.cond != nil {
		if list, err := s.cond.Conditions(ctx, plID, year, month); err == nil {
			for _, c := range list {
				condByCfo[c.CodeCFO] = c
			}
		}
	}
	// Доли по умолчанию из стратегии — до снимка: утверждённая версия читает
	// зафиксированные в ней доли, а не текущую стратегию.
	if mode == CalcInverse {
		platforms := make([]MarketplaceRow, 0)
		for _, mp := range MarketplaceSeed() {
			if segment == "" || mp.Segment == segment {
				platforms = append(platforms, mp)
			}
		}
		condByCfo = s.shareDefaults.Attach(ctx, condByCfo, platforms, year, month)
	}
	snapshotConditions, snapshotValues := s.mpClosedSnapshot(ctx, card, f.Currency)
	if snapshotConditions != nil {
		condByCfo = snapshotConditions
	}
	if mode == CalcInverse {
		f.Conditions = condByCfo
	}

	// Площадки задания + страна/сегмент. Задание может покрывать оба сегмента
	// (объединённая форма МП) — тогда шапка помечается «all», а разделение
	// остаётся внутри формы: колонки группируются по сегменту.
	countryOf := map[int]string{}
	leOf := map[int]string{}
	for _, m := range MarketplaceSeed() {
		countryOf[m.CodeCFO] = m.Country
		leOf[m.CodeCFO] = m.LegalEntity
	}
	segmentOf := s.segmentsByCfo(ctx, t.CfoCodes)
	// Сегмент, под которым SaveMpForm пишет итоговые строки задания (скидки,
	// уценки): по нему же они и читаются, независимо от выбранного разреза экрана.
	taskSegment := mpTotalsSegmentFor(scope, segmentOf, t.CfoCodes)
	// Разрез карточки сужает состав площадок: форма «Крупные МП» показывает
	// только крупные, даже если задание сгенерировано на обе группы.
	cfoCodes := t.CfoCodes
	if scope == "large" || scope == "small" {
		filtered := make([]int, 0, len(cfoCodes))
		for _, c := range cfoCodes {
			if segmentOf[c] == scope {
				filtered = append(filtered, c)
			}
		}
		cfoCodes = filtered
	}
	mpNames := s.nameMap(ctx, "dir_marketplace", "code_cfo", "name_cfo")
	cfoNames := s.nameMap(ctx, "dir_cfo", "code_cfo", "name_cfo")
	dirLE := s.nameMap(ctx, "dir_marketplace", "code_cfo", "legal_entity")
	for _, c := range cfoCodes {
		name := mpNames[c]
		if name == "" {
			name = cfoNames[c]
		}
		le := dirLE[c]
		if le == "" {
			le = leOf[c] // справочник может быть не наполнен — берём из seed
		}
		f.Platforms = append(f.Platforms, MpFormPlatform{
			CodeCFO: c, Name: name, Country: countryOf[c], Segment: segmentOf[c], LegalEntity: le,
		})
	}
	visibleSegments := map[int]string{}
	for _, cfo := range cfoCodes {
		visibleSegments[cfo] = segmentOf[cfo]
	}
	if segments := distinctSegments(visibleSegments); len(segments) > 1 {
		f.Segment = "all"
	}
	// Базис НДС по каждой площадке: отношение плановых сумм, ставка справочника —
	// фолбэк (mpform_vat.go). Одного числа на всю форму не существует: у площадок
	// ставки разные, поэтому шапка отдаёт и разрез VatByCfo, и признак VatMixed.
	segList := distinctSegments(visibleSegments)
	planLayer := mpPlanByBlock(ctx, s.fact, year, month, segList)
	prevY, prevM := prevMonth(year, month)
	planPrev := mpPlanByBlock(ctx, s.fact, prevY, prevM, segList)
	f.VatByCfo = map[int]MpVatBasis{}
	rateByCfo := map[int]float64{}
	for _, p := range f.Platforms {
		b := mpVatBasis(planLayer, planPrev, p.CodeCFO, year, month, s.vatFor(ctx, p))
		f.VatByCfo[p.CodeCFO] = b
		rateByCfo[p.CodeCFO] = b.Rate()
		f.VatFromPlan = f.VatFromPlan || b.FromPlan
		// Площадки без базы перечисляем поимённо: валидация МП-12 назовёт их в
		// тексте блокировки, а форма подсветит строку ставки.
		if b.Missing {
			f.VatMissing = append(f.VatMissing, p.CodeCFO)
		}
	}
	f.Vat, f.VatMixed = headerVat(rateByCfo)

	// Read-only сценарии из источника, уже в валюте формы: факт, факт прошлого
	// года, стратегия (FormToLoadPlan), таргет (FormToLoaTaktTarget).
	allowedCfo := map[int]bool{}
	for _, c := range cfoCodes {
		allowedCfo[c] = true
	}
	layerSet := s.mpLayersFor(ctx, year, month, segmentOf, allowedCfo, f.Currency)

	// Сохранённая тактика + стратегия + причины корректировок.
	tacByKey := map[string]float64{}
	manualByKey := map[string]bool{}
	stratByKey := map[string]float64{}
	reasonByKey := map[string]string{}
	// pl_metric хранит суммы в RUB; проценты (%СПП, наценка) валютой не пересчитываются.
	specByBlock := mpLineByBlock()
	toDisplay := func(block string, v float64) float64 {
		if specByBlock[block].ValueKind == ValuePct {
			return v
		}
		return convertAmount(v, "RUB", f.Currency)
	}
	if len(cfoCodes) > 0 {
		if rows, e := s.pool.Query(ctx, `
			SELECT profit_center, block_type, amount, is_manual, amount_strategy
			FROM pl_metric WHERE pl_id=$1 AND template_code='TPL-MP' AND profit_center = ANY($2)
			 -- Итоговые строки (скидки, уценки) хранятся под profit_center=0 и сегментом
			 -- задания (SaveMpForm): у крупных и мелких МП они свои, чужие не читаем.
			 -- Строки, сохранённые до разделения (segment = ''), читаются, только если
			 -- своей строки нет: она идёт последней и перекрывает их.
			 AND (profit_center <> 0 OR segment IN ($3, ''))
			 ORDER BY (profit_center = 0 AND segment = $3)`, t.PlID, append(cfoCodes, 0), taskSegment); e == nil {
			for rows.Next() {
				var pc int
				var bt string
				var amt, strat *float64
				var im bool
				if rows.Scan(&pc, &bt, &amt, &im, &strat) == nil {
					k := layerKey(pc, bt)
					if amt != nil {
						tacByKey[k] = toDisplay(bt, *amt)
						manualByKey[k] = im
					}
					_ = strat // Стратегия формы читается только из FormToLoadPlan за месяц тактики.
				}
			}
			rows.Close()
		}
		if ar, e := s.pool.Query(ctx, `
			SELECT profit_center, block_type, reason FROM pl_adjustment
			WHERE pl_id=$1 AND profit_center = ANY($2) ORDER BY adjusted_at`, t.PlID, append(cfoCodes, 0)); e == nil {
			for ar.Next() {
				var pc int
				var bt, rs string
				if ar.Scan(&pc, &bt, &rs) == nil {
					reasonByKey[fmt.Sprintf("%d:%s", pc, bt)] = rs
				}
			}
			ar.Close()
		}
	}

	// Онлайн-стратегия (FormToLoadPlan) перекрывает импортированную в pl_metric (TPL-09).
	for _, l := range layerSet.bySegment {
		for k, v := range l.strategy {
			stratByKey[k] = v
		}
	}

	spec := f.Lines
	editable := mpEditableSetFor(mode)

	// Тотал-строки (скидка/уценка) — по всем площадкам, profit_center=0.
	totalInputs := map[string]float64{}
	for _, l := range spec {
		if l.Scope == ScopeTotal && editable[l.BlockType] {
			if v, ok := tacByKey[fmt.Sprintf("0:%s", l.BlockType)]; ok {
				totalInputs[l.BlockType] = v
			}
		}
	}

	for _, p := range f.Platforms {
		// Слои берутся по сегменту КАЖДОЙ площадки: в объединённой форме
		// large и small приходят из разных срезов источника.
		pl := layerSet.forCfo(p.CodeCFO)
		norm := s.normShareOf(ctx, p.CodeCFO, year, month)
		if norm > 0 {
			if f.NormByCfo == nil {
				f.NormByCfo = map[int]float64{}
			}
			f.NormByCfo[p.CodeCFO] = norm
		}
		values := s.platformValues(mode, f.VatByCfo[p.CodeCFO], norm,
			condByCfo[p.CodeCFO], tacByKey, pl, p.CodeCFO)
		if snapshotValues[p.CodeCFO] != nil {
			values = snapshotValues[p.CodeCFO]
		}

		for _, l := range spec {
			if l.Kind == KindHeader {
				continue
			}
			k := layerKey(p.CodeCFO, l.BlockType)
			cell := MpFormCell{CodeCFO: p.CodeCFO, BlockType: l.BlockType}
			if mode == CalcInverse {
				cell.MissingShare = mpMissingShare(l, condByCfo[p.CodeCFO])
			}
			if l.Scope == ScopeTotal {
				cell.Value = totalInputs[l.BlockType]
			} else {
				cell.Value = values[l.BlockType]
			}
			// Факт (для input-строк) — из источника; рядом факт прошлого года.
			if l.Scope == ScopePlatform {
				if fv, ok := factSeed(pl.fact, l.BlockType, p.CodeCFO); ok {
					vv := fv
					cell.Fact = &vv
				}
				if pv, ok := factSeed(pl.factPrev, l.BlockType, p.CodeCFO); ok {
					vv := pv
					cell.FactPrev = &vv
				}
				if pv, ok := factSeed(pl.factPrevMonth, l.BlockType, p.CodeCFO); ok {
					vv := pv
					cell.FactPrevMonth = &vv
				}
			}
			if sv, ok := stratByKey[k]; ok {
				vv := sv
				cell.Strategy = &vv
			}
			if tv, ok := pl.target[k]; ok {
				vv := tv
				cell.Target = &vv
			}
			if tv, ok := tacByKey[k]; ok {
				vv := tv
				cell.Tactic = &vv
				cell.IsManual = manualByKey[k]
				if cell.IsManual {
					cell.Reason = reasonByKey[k]
				}
			}
			f.Cells = append(f.Cells, cell)
		}
	}

	// Отметка актуальности снимка факта (правка МП §12).
	if s.factSnap != nil {
		if meta, err := s.factSnap.Meta(ctx, year, month, SnapLayerFact, segment); err == nil {
			f.FactSnapshot = &meta
		} else {
			log.Printf("plans: отметка снимка факта МП недоступна: %v", err)
		}
	}

	// Подсказки к полям ввода (правки ТЗ, часть 1): фактический %СПП и габаритные
	// штрафы WB. Идут ОТДЕЛЬНО от ячеек — значение поля они не трогают.
	if s.hints != nil {
		for _, p := range f.Platforms {
			if h, ok := s.hints.SppHint(ctx, p.CodeCFO); ok {
				f.Hints = append(f.Hints, h)
			}
			if h, ok := s.hints.PenaltyHint(ctx, p.CodeCFO); ok {
				f.Hints = append(f.Hints, h)
			}
		}
	}
	return f, nil
}

// platformValues — значения строк по площадке в зависимости от режима расчёта.
//
// legacy: как было — суммы (тактика ?? факт) прогоняются через каскад
// «суммы → доли». inverse (ТЗ §3.1): вводится только объём продаж, а расходная
// часть считается из условий площадки; уже сохранённые ручные значения статей
// передаются как переопределения и автопересчётом не затираются (ТЗ §7.2).
func (s *TaskStore) platformValues(mode string, vat MpVatBasis, norm float64,
	cond MpConditions, tactic map[string]float64, layers mpLayers, cfo int) map[string]float64 {

	inputs := mpPlatformInputs(cfo, tactic, layers)
	if mode != CalcInverse {
		return computeMpPlatform(inputs, vat)
	}
	input := conditionsToInputNorm(cond, vat, norm)
	if spp, ok := tactic[layerKey(cfo, BSPP)]; ok {
		input.SppPct = spp
	}
	values, _ := computeMpPlatformInverse(MpInverseInput{
		SalesManagerGross: inputs[BSalesManagerGross],
		Conditions:        input,
	})
	return values
}

// vatFor — эффективная ставка НДС площадки: из справочника dir_vat, если он
// подключён, иначе законодательная по стране (ТЗ §3.4 запрещает прошивать её).
// Это ФОЛБЭК базиса: ведущее правило — отношение плановых сумм (mpform_vat.go).
func (s *TaskStore) vatFor(ctx context.Context, p MpFormPlatform) float64 {
	return s.vatRate(ctx, p.Country, p.CodeCFO)
}

// normShareOf — норматив удержаний площадки на месяц: справочник dir_mp_norm,
// иначе сид. Норматив зависит от периода, поэтому месяц обязателен.
func (s *TaskStore) normShareOf(ctx context.Context, cfo, year, month int) float64 {
	if s.rates != nil {
		if v, ok := s.rates.NormShare(ctx, cfo, year, month); ok {
			return v
		}
	}
	return mpNormShareOf(cfo, year, month)
}

// vatRate — та же ставка по паре «страна, ЦФО» (свод площадками не оперирует).
func (s *TaskStore) vatRate(ctx context.Context, country string, cfo int) float64 {
	if s.rates != nil {
		return s.rates.Vat(ctx, country, cfo)
	}
	return vatByCountry(country)
}

// mpPlatformInputs — входы каскада по площадке: сохранённая тактика ?? факт из
// источника ?? деривация (%СПП). Наценка по умолчанию расчётная. Одна функция на
// форму ввода и на свод — иначе проверяющий увидит не те числа, что заполняющий.
func mpPlatformInputs(cfo int, tactic map[string]float64, layers mpLayers) map[string]float64 {
	in := map[string]float64{}
	factIdx := layers.fact
	// Себестоимости больше не вводятся (их ведёт наценка), но СОХРАНЁННЫЕ суммы
	// прежних периодов читать обязаны: там наценки нет, и без этих значений
	// утверждённая форма пересчиталась бы в нули.
	for _, b := range []string{BShipments, BCogsTotal} {
		if v, ok := tactic[layerKey(cfo, b)]; ok {
			in[b] = v
		}
	}
	// Наценка от общей себестоимости по умолчанию берётся ИЗ ПЛАНА того же месяца
	// (прототип, строка 65: =BF65), а не вводится с нуля. Сохранённое значение
	// ниже её перебьёт.
	if v, ok := planMarkupTotal(layers.strategy, cfo); ok {
		in[BMarkupTotal] = v
	}
	for _, l := range mpFormSpec() {
		if l.Scope != ScopePlatform || !l.Editable {
			continue
		}
		if v, ok := tactic[layerKey(cfo, l.BlockType)]; ok {
			in[l.BlockType] = v
			continue
		}
		switch l.BlockType {
		case BSPP:
			// Поле ввода (правки ТЗ, часть 1 и МП §8): фактическое значение уходит
			// в подсказку рядом с полем, а не в само поле.
		case BCostPenalties:
			// Поле ввода (правка МП §14): общего реестра штрафов нет, а в DWH
			// лежат только габаритные штрафы Wildberries — это часть, не итог.
		case BMarkup:
			// расчётная по умолчанию — не сидируем
		case BSalesManagerGross:
			// Тактическая выручка МП в прототипе НЕ вводится: она приходит из
			// тактики ТО (=VLOOKUP([14]Янв_26!…)). Ближайший аналог в контуре —
			// слой таргета (FormToLoaTaktTarget), поэтому он старше факта: факт
			// прошлого месяца планом не является.
			if v, ok := layers.target[layerKey(cfo, l.BlockType)]; ok && v != 0 {
				in[l.BlockType] = v
			} else if v, ok := factSeed(factIdx, l.BlockType, cfo); ok {
				in[l.BlockType] = v
			}
		default:
			if v, ok := factSeed(factIdx, l.BlockType, cfo); ok {
				in[l.BlockType] = v
			}
		}
	}
	return in
}

// MpSaveRow — строка сохранения формы МП задания.
type MpSaveRow struct {
	CodeCFO   int     `json:"code_cfo"`
	BlockType string  `json:"block_type"`
	CodePL    int     `json:"code_pl"`
	Amount    float64 `json:"amount"`
	IsManual  bool    `json:"is_manual"`
	Comment   string  `json:"comment"`
}

// mpSaveScopeAllows checks one complete rule, including workflow stage, for the
// exact marketplace. Rules for different countries or stages cannot combine.
func mpSaveScopeAllows(scope ScopeFilter, stage string, cfo int) bool {
	if scope.Admin {
		return true
	}
	if cfo == 0 {
		for _, rule := range scope.Rules {
			if rule.StageCode == "" || rule.StageCode == stage {
				return true
			}
		}
		return false
	}
	var platform MarketplaceRow
	for _, candidate := range MarketplaceSeed() {
		if candidate.CodeCFO == cfo {
			platform = candidate
			break
		}
	}
	if platform.CodeCFO == 0 {
		return false
	}
	for _, rule := range scope.Rules {
		if (rule.StageCode == "" || rule.StageCode == stage) &&
			ruleAllows(rule, cfo, platform.Country, platform.LegalEntity) {
			return true
		}
	}
	return false
}

// SaveMpForm сохраняет ввод тактики задания МП (только editable-строки спеки) в
// pl_metric + снимок form_submission. Права: assignee/delegate/admin.
// currency — валюта, в которой пользователь ВВОДИЛ суммы; хранение всегда в RUB,
// поэтому денежные строки пересчитываются обратно (проценты — как есть).
func (s *TaskStore) SaveMpForm(ctx context.Context, taskID, actorID int64, isAdmin bool, rows []MpSaveRow, payload []byte, currency, formScope string) error {
	t, year, month, segment, err := s.taskBrief(ctx, taskID)
	if err != nil {
		return err
	}
	if !isAdmin && !(t.AssigneeID != nil && *t.AssigneeID == actorID) && !(t.DelegateID != nil && *t.DelegateID == actorID) {
		return fmt.Errorf("нет прав на заполнение задания")
	}
	scope := NewScopeFilter(isAdmin, nil)
	if !isAdmin {
		if s.scope == nil {
			return fmt.Errorf("срез доступа недоступен")
		}
		rules, scopeErr := s.scope.UserScopes(ctx, actorID)
		if scopeErr != nil {
			return fmt.Errorf("срез доступа недоступен: %w", scopeErr)
		}
		scope = NewScopeFilter(false, rules)
	}
	allowed := map[int]bool{}
	for _, c := range t.CfoCodes {
		allowed[c] = true
	}
	lineByBlock := mpLineByBlock()
	src := normalizeCurrency(currency)

	// Сегмент пишем по КАЖДОЙ площадке (объединённая форма покрывает оба);
	// тотал-строки (скидка/уценка) остаются под сегментом задания.
	segmentOf := s.segmentsByCfo(ctx, t.CfoCodes)
	segmentFor := func(cfo int) string {
		if seg, ok := segmentOf[cfo]; ok && seg != "" {
			return seg
		}
		return segment
	}
	// Значение «до корректировки» (ADJ-03) — факт в валюте хранения.
	layerSet := s.mpLayersFor(ctx, year, month, segmentOf, allowed, "RUB")
	type modeCard struct {
		mode string
		card Card
	}
	modeBySegment := map[string]modeCard{}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Блокируем карточки в стабильном порядке до первой записи. Переход
	// согласования ждёт эту транзакцию; проверка состояния ниже выполняется уже
	// под FOR UPDATE и не может устареть посреди сохранения.
	segments := map[string]bool{}
	for _, cfo := range t.CfoCodes {
		if seg := segmentFor(cfo); seg != "" {
			segments[seg] = true
		}
	}
	segmentList := make([]string, 0, len(segments))
	for seg := range segments {
		segmentList = append(segmentList, seg)
	}
	if (formScope == "large" || formScope == "small") && !segments[formScope] {
		return fmt.Errorf("разрез %s вне задания", formScope)
	}
	lockedRows, err := tx.Query(ctx, `
		SELECT id, scope_key, status, step_code, locked, calc_mode
		  FROM form_card WHERE pl_id=$1 AND form_code=$2 AND scope_key=ANY($3)
		 ORDER BY id FOR UPDATE`, t.PlID, TemplateMP, segmentList)
	if err != nil {
		return err
	}
	for lockedRows.Next() {
		var c Card
		c.FormCode = TemplateMP
		if err := lockedRows.Scan(&c.ID, &c.ScopeKey, &c.Status, &c.StepCode, &c.Locked, &c.CalcMode); err != nil {
			lockedRows.Close()
			return err
		}
		mode := c.CalcMode
		if cardEditable(c) {
			mode = CalcInverse
		}
		modeBySegment[c.ScopeKey] = modeCard{mode: mode, card: c}
	}
	err = lockedRows.Err()
	lockedRows.Close()
	if err != nil {
		return err
	}
	touchedCards := map[int64]bool{}
	for _, r := range rows {
		line, ok := lineByBlock[r.BlockType]
		if !ok {
			return fmt.Errorf("строка не редактируется: %s", r.BlockType)
		}
		pc := r.CodeCFO
		rowSegment := segment
		if line.Scope == ScopeTotal {
			pc = 0 // тотал-строки (скидка/уценка) — по всему заданию
			// Свои у крупных и мелких МП: под общим сегментом площадок задания
			// (раньше сегмент был пустым, и мелкие затирали скидки крупных).
			rowSegment = mpTotalsSegmentFor(formScope, segmentOf, t.CfoCodes)
		} else if !allowed[pc] {
			return fmt.Errorf("ЦФО %d вне задания", pc)
		} else {
			rowSegment = segmentFor(pc)
		}
		if !mpSaveScopeAllows(scope, t.StageCode, pc) {
			return fmt.Errorf("ЦФО %d вне вашего среза доступа", pc)
		}
		mc, found := modeBySegment[rowSegment]
		if !found {
			mc.mode = CalcInverse // период без карточки: новое направление по умолчанию
			modeBySegment[rowSegment] = mc
		}
		if !mpEditableSetFor(mc.mode)[r.BlockType] {
			return fmt.Errorf("строка не редактируется: %s", r.BlockType)
		}
		if mc.card.ID != 0 && !cardEditable(mc.card) {
			return fmt.Errorf("карточка %s закрыта на ввод тактики", rowSegment)
		}
		// Итог смешанного задания (крупные + мелкие) лежит под пустым сегментом, своей
		// карточки у него нет: закрытость проверяем по карточкам всех сегментов задания,
		// иначе скидки можно записать в утверждённый период.
		if rowSegment == "" && line.Scope == ScopeTotal {
			for seg, other := range modeBySegment {
				if seg != "" && other.card.ID != 0 {
					if !cardEditable(other.card) {
						return fmt.Errorf("карточка %s закрыта на ввод тактики", seg)
					}
					touchedCards[other.card.ID] = true
				}
			}
		}
		if mc.card.ID != 0 {
			touchedCards[mc.card.ID] = true
		}
		if r.IsManual && r.Comment == "" {
			return fmt.Errorf("причина корректировки обязательна (ADJ-02)")
		}
		if line.ValueKind == ValueMoney {
			r.Amount = convertAmount(r.Amount, src, "RUB")
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO pl_metric (pl_id, template_code, segment, line_code, block_type, profit_center, country, scenario, period_year, period_month, currency, amount, is_manual)
			VALUES ($1,'TPL-MP',$2,$3,$4,$5,'',$6,$7,$8,'RUB',$9,$10)
			ON CONFLICT (pl_id, template_code, segment, line_code, block_type, profit_center, scenario, period_year, period_month, currency)
			DO UPDATE SET amount=EXCLUDED.amount, is_manual=EXCLUDED.is_manual`,
			t.PlID, rowSegment, line.CodePL, r.BlockType, pc, ScenarioTactic, year, month, r.Amount, r.IsManual)
		if err != nil {
			return err
		}
		if r.IsManual {
			var orig *float64
			if v, ok := layerSet.forCfo(pc).fact[[2]int{pc, line.CodePL}]; ok {
				vv := v
				orig = &vv
			}
			_, _ = tx.Exec(ctx, `
				INSERT INTO pl_adjustment (pl_id, profit_center, line_code, block_type, period_year, period_month, currency, original_calculated, adjusted_value, reason, adjusted_by, adjusted_at)
				VALUES ($1,$2,$3,$4,$5,$6,'RUB',$7,$8,$9,$10,NOW())`,
				t.PlID, pc, line.CodePL, r.BlockType, year, month, orig, r.Amount, r.Comment, actorID)
		}
	}
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	_, _ = tx.Exec(ctx, `INSERT INTO form_submission (pl_id, template_code, json_payload, submitted_by, submitted_at) VALUES ($1,'TPL-MP',$2,$3,NOW())`, t.PlID, payload, actorID)
	_, _ = tx.Exec(ctx, `UPDATE pl_task SET status=CASE WHEN status='pending' THEN 'in_progress' ELSE status END, updated_at=NOW() WHERE id=$1`, taskID)
	for id := range touchedCards {
		if _, err := tx.Exec(ctx, `UPDATE form_card SET updated_at=GREATEST(clock_timestamp(), updated_at + INTERVAL '1 microsecond') WHERE id=$1`, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

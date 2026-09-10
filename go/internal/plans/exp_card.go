package plans

import (
	"context"
	"fmt"
	"time"
)

// Привязка формы этапа 1.5 к карточке процесса: снимок версии и строки для
// публикации (ТЗ §10, уточнения ответов 11, 34, 35 от 10.09.2026).
//
// Правила публикации:
//   - 1.5-Р публикует строки под кодом ЦФО магазина, 1.5-МП — под кодом ЦФО
//     площадки;
//   - строки отдела маркетплейсов идут под ЦФО 250;
//   - строки ИТ-реестра с признаком «перенести на ЦФО 250» публикуются под 250,
//     а не под своим кодом; с признаком «не учитывать» — не публикуются вовсе;
//   - реестр командировок раскладывается на ДВЕ статьи: билеты и командировочные
//     на 27, проживание на 30 (находка Н3);
//   - реестр логистики (статьи 51 и 52) публикуется под ЦФО 250 — и ТОЛЬКО
//     отсюда: в форме МП те же статьи остаются расчётным индикатором PL продаж
//     и в приёмник затрат не пишут (ответ 11).
//
// Свёртка по логическому ключу (Параметр + Страна + ЦФО + КодPL + Дата) делается
// общим механизмом публикации, поэтому несколько источников одной статьи под
// ЦФО 250 сложатся в одну строку сами.

// ExpCardProvider — снимок и публикация формы TPL-CFO-EXP.
type ExpCardProvider struct {
	repo   ExpRepo
	reg    ExpRegRepo
	common MpConditionsStore
}

// NewExpCardProvider — конструктор.
func NewExpCardProvider(repo ExpRepo, reg ExpRegRepo) *ExpCardProvider {
	return &ExpCardProvider{repo: repo, reg: reg}
}

// WithCommonCosts подключает реестр общих затрат МП.
//
// Ответ 34: `mp_common_cost` остаётся хранилищем, а форма 1.5 читает его как
// источник строк и публикует под ЦФО 250 — из формы продаж эта публикация
// убрана. Хранилище не переносим: реестр уже наполняется и версионируется, а
// миграция данных ради смены владельца ничего не улучшает.
func (p *ExpCardProvider) WithCommonCosts(s MpConditionsStore) *ExpCardProvider {
	p.common = s
	return p
}

// expPublishParam — значение колонки «Параметр» приёмника для затрат.
const expPublishParam = "ЗАТРАТЫ"

// FormSnapshot — снимок версии карточки: значения сетки и строки реестров.
//
// Реестры входят в снимок обязательно: итоги реестров попадают в статьи формы, и
// без их состава утверждённые суммы невозможно объяснить постфактум.
func (p *ExpCardProvider) FormSnapshot(ctx context.Context, c Card) (map[string]any, error) {
	in, err := p.repo.Instance(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	rows, _, err := p.repo.Rows(ctx, in.ID, ExpFilter{Limit: 5000})
	if err != nil {
		return nil, err
	}

	values := make([]map[string]any, 0, len(rows)*12)
	for _, r := range rows {
		for _, v := range r.Values {
			if v.Scenario != ExpScenarioPlan || v.Amount == nil {
				continue
			}
			values = append(values, map[string]any{
				"code_cfo": r.CodeCFO, "code_pl": r.CodePL, "month": v.Month,
				"amount": *v.Amount, "source": v.Source, "group_pl": r.GroupPL,
				"expense": r.Expense, "legal_entity": r.LegalEntity,
			})
		}
	}

	registries := map[string]any{}
	if p.reg != nil {
		for _, kind := range ExpRegKinds() {
			rr, err := p.reg.RegRows(ctx, in.ID, kind)
			if err != nil {
				continue
			}
			registries[kind] = rr
		}
	}

	return map[string]any{
		"instance":   in,
		"values":     values,
		"registries": registries,
		"plan_year":  in.PlanYear,
		"currency":   in.Currency,
	}, nil
}

// PublishRows — строки формы для приёмника (ТЗ §10).
func (p *ExpCardProvider) PublishRows(ctx context.Context, c Card) ([]PublishRow, error) {
	in, err := p.repo.Instance(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	rows, _, err := p.repo.Rows(ctx, in.ID, ExpFilter{Limit: 5000})
	if err != nil {
		return nil, err
	}

	out := make([]PublishRow, 0, len(rows)*12)
	mk := func(codeCFO, codePL, month int, name, group2 string, value float64) PublishRow {
		return PublishRow{
			BlockType: "exp",
			Param:     expPublishParam,
			Country:   in.Country,
			CodeCFO:   codeCFO,
			GroupCFO1: expKindGroup(in.FormKind),
			GroupCFO2: group2,
			CFOName:   name,
			CodePL:    codePL,
			Date:      time.Date(in.PlanYear, time.Month(month), 1, 0, 0, 0, 0, time.UTC),
			Value:     value,
			Currency:  in.Currency,
		}
	}

	// 1. Сетка формы: строка публикуется под своим кодом ЦФО.
	for _, r := range rows {
		if r.CodeCFO <= 0 || r.CodePL <= 0 {
			continue
		}
		for _, v := range r.Values {
			if v.Scenario != ExpScenarioPlan || v.Amount == nil || *v.Amount == 0 {
				continue
			}
			out = append(out, mk(r.CodeCFO, r.CodePL, v.Month, r.CFO, r.GroupCFO2, *v.Amount))
		}
	}

	if p.reg != nil {
		out = append(out, p.registryRows(ctx, in, mk)...)
	}

	// 2. Общие затраты МП — под ЦФО 250 (ответ 34). Только у формы
	//    маркетплейсов: у розницы отдела МП нет.
	if p.common != nil && in.FormKind == ExpKindMP {
		for _, seg := range []string{"large", "small"} {
			rr, err := p.common.CommonCosts(ctx, c.PlID, seg, in.PlanYear, c.Month)
			if err != nil {
				continue
			}
			for _, r := range rr {
				if r.CodePL <= 0 || r.Amount == 0 {
					continue
				}
				out = append(out, mk(ExpCFOMarketplaceDept, r.CodePL, c.Month,
					"Отдел маркетплейса", seg, r.Amount))
			}
		}
	}
	return out, nil
}

// registryRows — строки реестров для публикации.
func (p *ExpCardProvider) registryRows(ctx context.Context, in ExpInstance,
	mk func(codeCFO, codePL, month int, name, group2 string, value float64) PublishRow) []PublishRow {
	out := make([]PublishRow, 0, 64)

	// Командировки: билеты и командировочные — статья 27, проживание — 30 (Н3).
	if rr, err := p.reg.RegRows(ctx, in.ID, ExpRegTravel); err == nil {
		for _, r := range rr {
			if r.Excluded {
				continue
			}
			month := expAttrInt(r.Attrs, "month")
			if month < 1 || month > 12 {
				month = 1
			}
			if v := expAttrFloat(r.Attrs, "tickets") + expAttrFloat(r.Attrs, "per_diem"); v != 0 {
				out = append(out, mk(r.CodeCFO, ExpPLTravel, month, "", "", v))
			}
			if v := expAttrFloat(r.Attrs, "lodging"); v != 0 {
				out = append(out, mk(r.CodeCFO, ExpPLLodging, month, "", "", v))
			}
		}
	}

	// Логистика: статьи 51 и 52 под ЦФО 250 — и только отсюда (ответ 11).
	if rr, err := p.reg.RegRows(ctx, in.ID, ExpRegLogistics); err == nil {
		for _, r := range rr {
			if r.Excluded || r.CodePL <= 0 {
				continue
			}
			for m := 1; m <= 12; m++ {
				if v := r.MonthAmount(m); v != 0 {
					out = append(out, mk(ExpCFOMarketplaceDept, r.CodePL, m, "Отдел маркетплейса", "", v))
				}
			}
		}
	}

	// ИТ: признак «перенести на 250» меняет ЦФО, «не учитывать» снимает строку.
	if rr, err := p.reg.RegRows(ctx, in.ID, ExpRegIT); err == nil {
		for _, r := range rr {
			cfo, ok := ExpITPublishCFO(r)
			if !ok || cfo <= 0 {
				continue
			}
			codePL := r.CodePL
			if codePL <= 0 {
				codePL = ExpPLIT
			}
			name := ""
			if cfo == ExpCFOMarketplaceDept {
				name = "Отдел маркетплейса"
			}
			for m := 1; m <= 12; m++ {
				if v := r.MonthAmount(m); v != 0 {
					out = append(out, mk(cfo, codePL, m, name, "", v))
				}
			}
		}
	}
	return out
}

// ExpPublishDuplicates — пары «ЦФО + статья + месяц», которые приходят в
// публикацию из РАЗНЫХ наборов формы.
//
// Ответ 34 прямо требует: «перед включением проверить дубли между этими тремя
// наборами» — реестром общих затрат, статьями 51/52 логистики и строками ИТ с
// признаком переноса. Свёртка по логическому ключу сложит их в одну сумму молча,
// и расхождение с формой станет заметно только в приёмнике. Поэтому считаем их
// заранее и показываем в отчёте dry-run.
func ExpPublishDuplicates(rows []PublishRow) []string {
	type key struct {
		cfo, pl, month int
	}
	seen := map[key]int{}
	var out []string
	for _, r := range rows {
		k := key{r.CodeCFO, r.CodePL, int(r.Date.Month())}
		seen[k]++
		if seen[k] == 2 {
			out = append(out, fmt.Sprintf("ЦФО %d · статья %d · месяц %d приходит из двух наборов",
				k.cfo, k.pl, k.month))
		}
	}
	return out
}

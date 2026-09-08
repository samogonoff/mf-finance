package plans

import "context"

// Ставка НДС формы МП не задаётся ни константой, ни справочником.
//
// В прототипе её нет вовсе: суммы «без НДС» получаются делением на ОТНОШЕНИЕ
// плановых сумм того же месяца и той же площадки — лист «3. МП — конструктор
// тактики», строки 16 и 31: `=AR12/(BF12/BF17)` и `=AR27/(BF27/BF32)`. Отношение
// выходит разным по площадкам и по месяцам: июнь 2026 Wildberries 1,203592
// (20,3592 %), итог по четырём площадкам 1,203266.
//
// До этого форма делила на `1 + ставка` (справочник dir_vat, фолбэк
// vatByCountry = 20 %), из-за чего на экране шапка показывала 19,60 %, расчёт шёл
// по 20 %, а справочник статей VAT_RU хранил 22 % — три разных числа для одной
// ставки, и ошибка уезжала во всё производное (продажи без НДС, себестоимость,
// маржа, PL). Разбор: var/Формулы_форм_07.09.2026.xlsx (лист «5. Сверка с
// кабинетом», п. 2) и var/Найдено_в_кабинете_07.09.2026.docx (часть 2).

// MpVatBasis — как форма переводит суммы «с НДС» в «без НДС» по одной площадке.
//
// Ставка ОДНА на оба уровня цены (правка 08.09.2026, лист A п. 4: «то же
// отношение работает для пары 1022 → 1006»). Прежняя версия держала две
// независимые пары отношений — по прототипу, где каждая строка делится на своё;
// заказчик это отменил, и на одной ставке контроль связности
// `1 − [1006]/[1045] = %СПП` сходится по определению, а на двух — нет.
type MpVatBasis struct {
	Ratio    float64 `json:"ratio"`     // план 1046 / план 1045 площадки
	FromPlan bool    `json:"from_plan"` // отношение получено из плана, а не из справочника
	// Year/Month — период плана, из которого взято отношение: месяц формы либо
	// предыдущий. Форма подписывает это рядом со ставкой, иначе непонятно, почему
	// у соседних площадок ставки разных месяцев.
	Year  int `json:"year,omitempty"`
	Month int `json:"month,omitempty"`
	// Missing — базы для расчёта ставки нет ни в текущем, ни в предыдущем месяце.
	// Значения при этом считаются по ставке справочника (иначе форму нельзя было
	// бы открыть ни в mock-режиме, ни на периоде без плана), но отправка на
	// согласование блокируется валидацией МП-12: план утверждают по числам, а не
	// по аварийному фолбэку.
	Missing bool `json:"missing"`
}

// Rate — эффективная ставка, которую даёт отношение: её показывает форма и
// фиксирует версия карточки.
func (b MpVatBasis) Rate() float64 {
	if b.Ratio <= 0 {
		return 0
	}
	return b.Ratio - 1
}

// vatBasisFromRate — базис из ставки: способ учесть ставку, введённую руками в
// условиях площадки, и аварийный фолбэк на отсутствующий план.
func vatBasisFromRate(vat float64) MpVatBasis {
	return MpVatBasis{Ratio: 1 + vat}
}

// Границы правдоподобия отношения. Ставка НДС контура лежит в 0…22 %, поэтому
// отношение вне 1,00…1,50 означает не «высокий НДС», а мусор в источнике: чаще
// всего в плановую пару попали разные периоды или разные единицы. Делить на такое
// отношение нельзя — суммы уедут молча.
const (
	mpVatRatioMin = 1.0
	mpVatRatioMax = 1.5
)

// mpPlanRatio — отношение план_1046 ÷ план_1045 площадки в одном месяце.
func mpPlanRatio(plan map[string]float64, cfo int) (float64, bool) {
	g, n := plan[layerKey(cfo, BSalesManagerGross)], plan[layerKey(cfo, BSalesManagerNet)]
	if g <= 0 || n <= 0 {
		return 0, false
	}
	r := g / n
	if r < mpVatRatioMin || r > mpVatRatioMax {
		return 0, false
	}
	return r, true
}

// mpVatBasis — базис площадки: отношение из плана месяца формы, иначе из плана
// ПРЕДЫДУЩЕГО месяца по той же площадке (правка 08.09.2026 п. 1), иначе пометка
// Missing и ставка справочника как аварийное значение.
func mpVatBasis(plan, planPrev map[string]float64, cfo, year, month int, fallbackVat float64) MpVatBasis {
	if r, ok := mpPlanRatio(plan, cfo); ok {
		return MpVatBasis{Ratio: r, FromPlan: true, Year: year, Month: month}
	}
	py, pm := prevMonth(year, month)
	if r, ok := mpPlanRatio(planPrev, cfo); ok {
		return MpVatBasis{Ratio: r, FromPlan: true, Year: py, Month: pm}
	}
	b := vatBasisFromRate(fallbackVat)
	b.Missing = true
	return b
}

// mpPlanByBlock — плановый слой (FormToLoadPlan) по сегментам, свёрнутый в
// block_type: из него берутся пары «с НДС / без НДС» для отношений. Суммы НЕ
// приводятся к валюте отображения — отношение безразмерно, а обе суммы пары
// приходят из источника в одной валюте.
//
// Сегментов может быть два (объединённая форма МП): план в Budgeting лежит в
// разрезе групп 250/480, поэтому у large и small это разные запросы.
func mpPlanByBlock(ctx context.Context, src MpFactSource, year, month int, segments []string) map[string]float64 {
	out := map[string]float64{}
	if src == nil {
		return out
	}
	pb := plToBlock()
	seen := map[string]bool{}
	for _, seg := range segments {
		if seg == "" || seen[seg] {
			continue
		}
		seen[seg] = true
		rows, err := src.MpStrategy(ctx, year, month, seg)
		if err != nil {
			continue // источник недоступен — работает фолбэк-ставка
		}
		for _, r := range rows {
			if bt := pb[r.CodePL]; bt != "" {
				out[layerKey(r.CodeCFO, bt)] += r.Amount
			}
		}
	}
	return out
}

// headerVat — ставка для шапки формы и признак «ставки площадок разные».
// Разброс больше 0,05 п.п. считаем разными ставками: в пределах этого суммы
// округления, а не разная налоговая база.
func headerVat(byCfo map[int]float64) (float64, bool) {
	if len(byCfo) == 0 {
		return 0, false
	}
	min, max := 0.0, 0.0
	first := true
	for _, v := range byCfo {
		if first {
			min, max, first = v, v, false
			continue
		}
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return min, max-min > 0.0005
}

// planMarkupTotal — наценка от общей себестоимости по плану того же месяца:
// прототип берёт её ссылкой на план (строка 65: =BF65), а не вводит.
// Возвращает false, если пары сумм в плане нет — тогда наценку вводят руками.
func planMarkupTotal(plan map[string]float64, cfo int) (float64, bool) {
	platNet := plan[layerKey(cfo, BSalesPlatNet)]
	cogs := plan[layerKey(cfo, BCogsTotal)]
	if platNet <= 0 || cogs <= 0 {
		return 0, false
	}
	return platNet/cogs - 1, true
}

// mpPlanByPL — плановый слой по КОДАМ статей (а не по block_type): нужен там, где
// один блок формы собран из нескольких кодов. Такой блок один — себестоимость
// общая (2006 основная + 6006 пошив), и при публикации её надо развести обратно.
func mpPlanByPL(ctx context.Context, src MpFactSource, year, month int, segments []string) map[[2]int]float64 {
	out := map[[2]int]float64{}
	if src == nil {
		return out
	}
	seen := map[string]bool{}
	for _, seg := range segments {
		if seg == "" || seen[seg] {
			continue
		}
		seen[seg] = true
		rows, err := src.MpStrategy(ctx, year, month, seg)
		if err != nil {
			continue
		}
		for _, r := range rows {
			out[[2]int{r.CodeCFO, r.CodePL}] += r.Amount
		}
	}
	return out
}

// splitCogsByPlan — разделение себестоимости общей на 2006 и 6006 пропорцией
// плана того же месяца (правка 08.09.2026 п. 4). Пошив считается ВЫЧИТАНИЕМ,
// чтобы сумма двух записей сошлась с показанной строкой до копейки.
//
// Плана нет — вся сумма относится на 2006: форма покажет пометку «разделение
// недоступно», а в приёмник уйдёт одна строка вместо двух неверных.
func splitCogsByPlan(total float64, plan map[[2]int]float64, cfo int) (main, sewing float64) {
	p2006, p6006 := plan[[2]int{cfo, 2006}], plan[[2]int{cfo, 6006}]
	sum := p2006 + p6006
	if sum <= 0 || p2006 <= 0 {
		return total, 0
	}
	main = total * p2006 / sum
	return main, total - main
}

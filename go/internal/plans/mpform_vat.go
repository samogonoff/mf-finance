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
// Отношения, а не ставка: ставка — производная от них (Rate), и хранить её как
// ведущую величину значит терять разницу между уровнями «цена менеджера» и
// «цена площадки», у которых в источнике свои плановые пары.
type MpVatBasis struct {
	ManagerRatio float64 `json:"manager_ratio"` // план 1046 / план 1045
	PlatRatio    float64 `json:"plat_ratio"`    // план 1022 / план 1006
	FromPlan     bool    `json:"from_plan"`     // отношение получено из плана, а не из справочника
}

// Rate — эффективная ставка, которую даёт отношение: её показывает шапка формы и
// фиксирует версия карточки. Считается по уровню цены менеджера — на нём стоит
// ввод, и именно эту ставку видит заполняющий.
func (b MpVatBasis) Rate() float64 {
	if b.ManagerRatio <= 0 {
		return 0
	}
	return b.ManagerRatio - 1
}

// vatBasisFromRate — базис из ставки: фолбэк на случай, когда плана нет (или его
// суммы непригодны), и способ учесть ставку, введённую руками в условиях площадки.
func vatBasisFromRate(vat float64) MpVatBasis {
	return MpVatBasis{ManagerRatio: 1 + vat, PlatRatio: 1 + vat}
}

// Границы правдоподобия отношения. Ставка НДС контура лежит в 0…22 %, поэтому
// отношение вне 1,00…1,50 означает не «высокий НДС», а мусор в источнике: чаще
// всего в плановую пару попали разные периоды или разные единицы. Делить на такое
// отношение нельзя — суммы уедут молча, поэтому берём фолбэк-ставку.
const (
	mpVatRatioMin = 1.0
	mpVatRatioMax = 1.5
)

// mpVatBasis — базис площадки из планового слоя (FormToLoadPlan, свёрнут по
// block_type). Пара, которой в плане нет, заменяется другой, а если нет обеих —
// работает ставка справочника: без плана делить не на что.
func mpVatBasis(plan map[string]float64, cfo int, fallbackVat float64) MpVatBasis {
	ratio := func(gross, net string) (float64, bool) {
		g, n := plan[layerKey(cfo, gross)], plan[layerKey(cfo, net)]
		if g <= 0 || n <= 0 {
			return 0, false
		}
		r := g / n
		if r < mpVatRatioMin || r > mpVatRatioMax {
			return 0, false
		}
		return r, true
	}
	mgr, okMgr := ratio(BSalesManagerGross, BSalesManagerNet)
	plat, okPlat := ratio(BSalesPlatGross, BSalesPlatNet)
	switch {
	case okMgr && okPlat:
		return MpVatBasis{ManagerRatio: mgr, PlatRatio: plat, FromPlan: true}
	case okMgr:
		return MpVatBasis{ManagerRatio: mgr, PlatRatio: mgr, FromPlan: true}
	case okPlat:
		return MpVatBasis{ManagerRatio: plat, PlatRatio: plat, FromPlan: true}
	default:
		return vatBasisFromRate(fallbackVat)
	}
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

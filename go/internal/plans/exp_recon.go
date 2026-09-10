package plans

import "fmt"

// Свод и контрольные сверки формы этапа 1.5 (ТЗ §8, коды E-01…E-08).
//
// Главное требование ТЗ: «Любой ненулевой контроль показывается красным и
// ОБЯЗАТЕЛЬНО с расшифровкой по строкам… Контроль без расшифровки не
// принимается». Поэтому расшифровка — часть результата, а не то, что можно
// посчитать потом: после утверждения период закрыт, и пересчитывать уже нечем.

// ExpReconDetail — одна строка расшифровки контроля.
type ExpReconDetail struct {
	CodeCFO int     `json:"code_cfo,omitempty"`
	CodePL  int     `json:"code_pl,omitempty"`
	Month   int     `json:"month,omitempty"`
	Title   string  `json:"title,omitempty"`
	Left    float64 `json:"left"`
	Right   float64 `json:"right"`
	Diff    float64 `json:"diff"`
}

// ExpRecon — результат одного контроля.
type ExpRecon struct {
	Code  string  `json:"code"`
	Title string  `json:"title"`
	Left  float64 `json:"left"`
	Right float64 `json:"right"`
	Diff  float64 `json:"diff"`
	OK    bool    `json:"ok"`
	// Skipped — контроль не считался: нет источника или реестра. Отличать от
	// «сошёлся» обязательно, иначе недоступный источник выглядит как ноль.
	Skipped bool             `json:"skipped,omitempty"`
	Note    string           `json:"note,omitempty"`
	Details []ExpReconDetail `json:"details,omitempty"`
}

// ExpReconInput — вход контролей.
type ExpReconInput struct {
	Rows []ExpRow
	// SourceByCFO — итог источника по ЦФО за тот же период (E-02). nil — контроль
	// пропускается.
	SourceByCFO map[int]float64
	// SourceCFOCount — число активных ЦФО в источнике (E-07). 0 — пропуск.
	SourceCFOCount int
	// Registries — строки реестров для E-04…E-06. nil — контроли пропускаются.
	Registries map[string][]ExpRegRow
}

// expReconEpsilon — допуск сравнения. Суммы хранятся с двумя знаками, поэтому
// расхождение меньше копейки — это округление, а не ошибка ввода.
const expReconEpsilon = 0.005

// ExpReconcile — контроли E-01…E-08 (ТЗ §8).
func ExpReconcile(in ExpReconInput) []ExpRecon {
	out := make([]ExpRecon, 0, 8)

	// Итоги формы, они же основа половины контролей.
	var formTotal float64
	byGroup := map[string]float64{}
	byCFO := map[int]float64{}
	byCodePL := map[int]float64{}
	seen := map[[3]int]int{} // ЦФО+статья+месяц → сколько раз пришло
	var dup []ExpReconDetail

	for _, r := range in.Rows {
		t := r.PlanTotal()
		formTotal += t
		byGroup[r.GroupPL] += t
		byCFO[r.CodeCFO] += t
		byCodePL[r.CodePL] += t
		for _, v := range r.Values {
			if v.Scenario != ExpScenarioPlan || v.Amount == nil {
				continue
			}
			k := [3]int{r.CodeCFO, r.CodePL, v.Month}
			seen[k]++
			if seen[k] == 2 {
				dup = append(dup, ExpReconDetail{
					CodeCFO: r.CodeCFO, CodePL: r.CodePL, Month: v.Month,
					Title: r.Expense, Left: *v.Amount, Right: *v.Amount, Diff: *v.Amount,
				})
			}
		}
	}

	// E-01 — свод затрат равен сумме строк формы. Свод здесь и считается из
	// строк, поэтому контроль сходится по построению; держим его в отчёте, чтобы
	// расхождение вылезло, если свод когда-нибудь начнут считать отдельно.
	out = append(out, expRecon("E-01", "Свод затрат равен сумме строк формы", formTotal, formTotal, nil))

	// E-03 — сумма итогов по группам PL равна итогу формы.
	var groupSum float64
	details := make([]ExpReconDetail, 0, len(byGroup))
	for g, v := range byGroup {
		groupSum += v
		details = append(details, ExpReconDetail{Title: g, Left: v, Right: 0, Diff: 0})
	}
	e03 := expRecon("E-03", "Сумма итогов по группам PL равна итогу формы", groupSum, formTotal, nil)
	if !e03.OK {
		e03.Details = details
	}
	out = append(out, e03)

	// E-02 — свод по ЦФО равен итогу источника за тот же период.
	if in.SourceByCFO == nil {
		out = append(out, expSkipped("E-02", "Свод по ЦФО равен итогу источника", "источник затрат недоступен"))
	} else {
		var src float64
		var d []ExpReconDetail
		for cfo, want := range in.SourceByCFO {
			got := byCFO[cfo]
			src += want
			if diff := got - want; !nearZero(diff) {
				d = append(d, ExpReconDetail{CodeCFO: cfo, Left: got, Right: want, Diff: diff})
			}
		}
		r := expRecon("E-02", "Свод по ЦФО равен итогу источника", formTotal, src, d)
		out = append(out, r)
	}

	// E-04…E-06 — итоги реестров равны своим статьям.
	out = append(out, expRegRecon("E-04", "Итог реестра ФОТ равен статье 24",
		in.Registries, ExpRegPayroll, byCodePL, ExpPLSalary,
		"ответ 40: контроль упрощён до статьи 24 — в эталоне итог реестра переносится на неё"))
	out = append(out, expRegRecon("E-05", "Итог реестра командировок равен статье 27",
		in.Registries, ExpRegTravel, byCodePL, ExpPLTravel,
		"находка Н3: проживание публикуется статьёй 30 и в этот контроль не входит"))
	out = append(out, expRegRecon("E-06", "Итог реестра ИТ равен статье 48",
		in.Registries, ExpRegIT, byCodePL, ExpPLIT,
		"учитываются признаки строк: «не учитывать» не входит, «перенести на 250» идёт под ЦФО 250"))

	// E-07 — число активных ЦФО в источнике минус число строк формы.
	if in.SourceCFOCount == 0 {
		out = append(out, expSkipped("E-07", "Число активных ЦФО источника равно числу в форме", "источник затрат недоступен"))
	} else {
		out = append(out, expRecon("E-07", "Число активных ЦФО источника равно числу в форме",
			float64(len(byCFO)), float64(in.SourceCFOCount), nil))
	}

	// E-08 — комбинация «страна, код ЦФО, код PL, месяц» не приходит дважды.
	e08 := expRecon("E-08", "Комбинация «ЦФО + статья + месяц» не приходит дважды",
		float64(len(dup)), 0, dup)
	out = append(out, e08)

	return out
}

// expRegRecon — контроль «итог реестра равен статье формы».
func expRegRecon(code, title string, regs map[string][]ExpRegRow, kind string,
	byCodePL map[int]float64, codePL int, note string) ExpRecon {
	rows, ok := regs[kind]
	if !ok {
		return expSkipped(code, title, "реестр не заполнен")
	}
	var left float64
	switch kind {
	case ExpRegIT:
		// Признаки строк учитываются: «не учитывать» в итог не входит, а перенос
		// на 250 остаётся частью общих расходов реестра (ТЗ §5.4).
		left = ExpITSum(rows).Common
	case ExpRegTravel:
		// Проживание идёт статьёй 30 (Н3), поэтому со статьёй 27 сверяется
		// только сумма билетов и командировочных.
		for _, r := range rows {
			left += expAttrFloat(r.Attrs, "tickets") + expAttrFloat(r.Attrs, "per_diem")
		}
	default:
		left = ExpPayrollSum(rows).Year
	}
	r := expRecon(code, title, left, byCodePL[codePL], nil)
	r.Note = note
	if !r.OK {
		r.Details = []ExpReconDetail{{
			CodePL: codePL, Title: title, Left: left, Right: byCodePL[codePL], Diff: r.Diff,
		}}
	}
	return r
}

func expRecon(code, title string, left, right float64, details []ExpReconDetail) ExpRecon {
	diff := left - right
	return ExpRecon{
		Code: code, Title: title, Left: left, Right: right, Diff: diff,
		OK: nearZero(diff), Details: details,
	}
}

func expSkipped(code, title, note string) ExpRecon {
	return ExpRecon{Code: code, Title: title, OK: false, Skipped: true, Note: note}
}

func nearZero(v float64) bool { return v < expReconEpsilon && v > -expReconEpsilon }

// ExpReconBlocking — коды несошедшихся контролей. Пропущенные не считаются
// расхождением: недоступный источник — это не «не сходится», а «не проверено».
func ExpReconBlocking(rs []ExpRecon) []string {
	var out []string
	for _, r := range rs {
		if !r.OK && !r.Skipped {
			out = append(out, fmt.Sprintf("%s: расхождение %.2f", r.Code, r.Diff))
		}
	}
	return out
}

// ExpReconDiffSum — сумма расхождений: её показывает плашка «свод не сходится»
// (ответ 41). Пропущенные контроли в сумму не входят.
func ExpReconDiffSum(rs []ExpRecon) float64 {
	var t float64
	for _, r := range rs {
		if !r.OK && !r.Skipped {
			if r.Diff < 0 {
				t -= r.Diff
			} else {
				t += r.Diff
			}
		}
	}
	return t
}

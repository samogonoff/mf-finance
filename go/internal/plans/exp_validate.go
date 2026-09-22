package plans

import (
	"fmt"
	"math"
	"strings"
)

// Валидации формы этапа 1.5 (ТЗ §9, коды E-11…E-20).
//
// Чистые функции над уже собранными данными: ни БД, ни HTTP. Так проверки
// покрываются табличными тестами, а сервис только подаёт им вход.
//
// Разделение на блокирующие и предупреждения — из ТЗ. Блокирует ли блокирующее
// замечание отправку на самом деле, решает режим гейта (ответ 41): по умолчанию
// мягкий — считаем всегда, показываем всегда, процесс не держим.

// ExpIssue — одно замечание формы.
type ExpIssue struct {
	Code     string  `json:"code"`
	Blocking bool    `json:"blocking"`
	CodeCFO  int     `json:"code_cfo,omitempty"`
	CodePL   int     `json:"code_pl,omitempty"`
	Month    int     `json:"month,omitempty"`
	Message  string  `json:"message"`
	Value    float64 `json:"value,omitempty"`
}

// ExpValidationInput — вход проверок.
type ExpValidationInput struct {
	Instance ExpInstance
	Rows     []ExpRow
	// PrevYearByRow — итог статьи за прошлый год по ключу «ЦФО:статья»: база
	// сравнения для E-18. Пусто — сравнивать не с чем, предупреждение не ставим.
	PrevYearByRow map[[2]int]float64
	// CommentByRow — комментарии строк: E-18 требует комментарий к строке.
	CommentByRow map[[2]int]string
	// ExpenseByCodePL — наименования статей справочника для E-17.
	ExpenseByCodePL map[int]string
	// LegalEntity — единственное ЮЛ страны формы (E-13).
	LegalEntity string
	// Currency — национальная валюта страны формы (E-15).
	Currency string
}

// Минус допустим В ЛЮБОЙ статье (ответ И-2 от 11.09.2026).
//
// Белого списка групп больше нет: аналитик нашёл отрицательные суммы в семи
// группах из восьми, включая те, где прежняя реализация их блокировала — коды
// 10, 54, 66, 99, 100, в сумме больше 34 миллионов. Блокировать законный ввод
// нельзя, но и молча пропускать минус тоже: E-12 стал ПРЕДУПРЕЖДЕНИЕМ, которое
// требует комментария к строке, как и E-18.
//
// expDeviationLimit — порог предупреждения E-18: отклонение от прошлого периода
// больше 30 %.
const expDeviationLimit = 0.30

// ValidateExpForm — проверки E-11…E-18 по форме (E-19 и E-20 живут на вводе
// реестров: там они и должны срабатывать, а не через сутки на своде).
func ValidateExpForm(in ExpValidationInput) []ExpIssue {
	out := make([]ExpIssue, 0, 16)
	add := func(code string, blocking bool, r ExpRow, month int, value float64, format string, args ...any) {
		out = append(out, ExpIssue{
			Code: code, Blocking: blocking, CodeCFO: r.CodeCFO, CodePL: r.CodePL,
			Month: month, Value: value, Message: fmt.Sprintf(format, args...),
		})
	}

	// E-15 — валюта ввода равна национальной валюте страны формы. Проверка на
	// уровне формы: если валюта не та, все суммы неверны, и построчно это
	// повторять бессмысленно.
	if in.Currency != "" && in.Instance.Currency != "" && !strings.EqualFold(in.Currency, in.Instance.Currency) {
		out = append(out, ExpIssue{Code: "E-15", Blocking: true,
			Message: fmt.Sprintf("валюта ввода %s не совпадает с национальной валютой страны формы (%s)",
				in.Instance.Currency, in.Currency)})
	}

	for _, r := range in.Rows {
		filled := map[int]bool{}
		// Одно предупреждение на строку: минус в двенадцати месяцах подряд — это
		// одна ситуация, а не двенадцать.
		negativeSeen := false
		for _, v := range r.Values {
			if v.Scenario != ExpScenarioPlan || v.Amount == nil {
				continue
			}
			filled[v.Month] = true

			// E-14 — значения не заданы вне планового периода.
			if v.Month < 1 || v.Month > 12 {
				add("E-14", true, r, v.Month, *v.Amount, "значение задано вне планового периода: месяц %d", v.Month)
			}
			// E-12 — отрицательная сумма: предупреждение с обязательным
			// комментарием (ответ И-2), а не блокировка.
			if *v.Amount < 0 && !negativeSeen {
				negativeSeen = true
				msg := "отрицательная сумма в статье «%s» — нужен комментарий к строке"
				if strings.TrimSpace(in.CommentByRow[[2]int{r.CodeCFO, r.CodePL}]) != "" {
					msg = "отрицательная сумма в статье «%s» — комментарий дан"
				}
				add("E-12", false, r, v.Month, *v.Amount, msg, r.Expense)
			}
		}

		// E-11 — заполнены ВСЕ плановые месяцы; пусто не равно нулю.
		var missing []int
		for m := 1; m <= 12; m++ {
			if !filled[m] {
				missing = append(missing, m)
			}
		}
		if len(missing) > 0 {
			add("E-11", true, r, missing[0], 0,
				"не заполнено месяцев: %d (пусто не равно нулю — для нуля нужен явный ввод)", len(missing))
		}

		// E-13 — все строки формы отнесены на одно юридическое лицо страны.
		if in.LegalEntity != "" && r.LegalEntity != "" && r.LegalEntity != in.LegalEntity {
			add("E-13", true, r, 0, 0,
				"строка отнесена на ЮЛ «%s», а форма — на «%s»", r.LegalEntity, in.LegalEntity)
		}

		// E-17 — статья против справочника.
		//
		// Ответ И-10 (11.09.2026): блокирует ТОЛЬКО отсутствие кода в справочнике
		// целиком. Расхождение наименования и незаданный второй уровень — это
		// предупреждения: появление нового кода PL не должно останавливать
		// отправку, а незаполненная укрупнённая группа уводит код в «Не
		// распределено» и публикацию не держит.
		if len(in.ExpenseByCodePL) > 0 {
			want, known := in.ExpenseByCodePL[r.CodePL]
			switch {
			case !known:
				add("E-17", true, r, 0, 0,
					"кода PL %d нет в справочнике статей — строку не на что отнести", r.CodePL)
			case want != "" && r.Expense != "" && want != r.Expense:
				add("E-17", false, r, 0, 0,
					"наименование статьи «%s» расходится со справочником: «%s»", r.Expense, want)
			}
		}

		// E-18 — отклонение от предыдущего периода больше 30 %: предупреждение,
		// требуется комментарий к строке.
		prev, hasPrev := in.PrevYearByRow[[2]int{r.CodeCFO, r.CodePL}]
		if hasPrev && prev != 0 {
			dev := (r.PlanTotal() - prev) / math.Abs(prev)
			if math.Abs(dev) > expDeviationLimit {
				msg := "отклонение от прошлого периода %.0f %% — нужен комментарий к строке"
				if strings.TrimSpace(in.CommentByRow[[2]int{r.CodeCFO, r.CodePL}]) != "" {
					msg = "отклонение от прошлого периода %.0f %% — комментарий дан"
				}
				add("E-18", false, r, 0, dev*100, msg, dev*100)
			}
		}
	}
	return out
}

// ExpBlocking — только блокирующие замечания.
func ExpBlocking(issues []ExpIssue) []ExpIssue {
	out := make([]ExpIssue, 0, len(issues))
	for _, i := range issues {
		if i.Blocking {
			out = append(out, i)
		}
	}
	return out
}

// ExpWarnings — только предупреждения.
func ExpWarnings(issues []ExpIssue) []ExpIssue {
	out := make([]ExpIssue, 0, len(issues))
	for _, i := range issues {
		if !i.Blocking {
			out = append(out, i)
		}
	}
	return out
}

// ExpUnclosedComments — предупреждения E-18 без комментария к строке. Их число
// форма показывает в шапке (ТЗ §7.8).
func ExpUnclosedComments(issues []ExpIssue, comments map[[2]int]string) int {
	n := 0
	for _, i := range issues {
		if i.Code != "E-18" {
			continue
		}
		if strings.TrimSpace(comments[[2]int{i.CodeCFO, i.CodePL}]) == "" {
			n++
		}
	}
	return n
}

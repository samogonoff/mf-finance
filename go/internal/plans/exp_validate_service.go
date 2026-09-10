package plans

import (
	"context"
	"log"
)

// Сборка отчёта проверок формы 1.5: валидации §9 и контроли §8 в одном месте.
//
// Отчёт нужен трём потребителям сразу — форме, своду и гейту отправки, — и
// собирать его по-разному для каждого нельзя: разошлись бы кнопка и плашка над
// ней (ровно этим болела форма розницы до гейта).

// ExpValidationReport — отчёт GET /api/plans/exp/{cardId}/validate.
type ExpValidationReport struct {
	CardID   int64      `json:"card_id"`
	Blocking []ExpIssue `json:"blocking"`
	Warnings []ExpIssue `json:"warnings"`
	Recon    []ExpRecon `json:"reconciliations"`
	// CanSubmit — можно ли отправлять. В мягком режиме остаётся true при
	// непустом blocking: замечания показываются, но процесс не держат (ответ 41).
	CanSubmit bool `json:"can_submit"`
	Strict    bool `json:"strict"`
	// ReconDiff — сумма расхождений контролей: её показывает плашка «свод не
	// сходится» и заполняющему, и согласующему (ответ 41, пункты 3 и 4).
	ReconDiff float64 `json:"recon_diff"`
	// UnclosedComments — предупреждения E-18 без комментария к строке (ТЗ §7.8).
	UnclosedComments int `json:"unclosed_comments"`
}

// Validate — проверки и контроли формы.
func (s *ExpService) Validate(ctx context.Context, p Principal, cardID int64) (ExpValidationReport, error) {
	c, in, err := s.access(ctx, cardID)
	if err != nil {
		return ExpValidationReport{}, err
	}
	// Проверяем ВСЮ форму, а не видимый срез отправителя: иначе ответственный с
	// узким срезом «проходил» бы проверку, потому что чужие незаполненные строки
	// ему не видны (та же ошибка уже ловилась в рознице).
	rows, _, err := s.repo.Rows(ctx, in.ID, ExpFilter{Limit: 5000})
	if err != nil {
		return ExpValidationReport{}, err
	}

	vin := ExpValidationInput{
		Instance:      in,
		Rows:          rows,
		Currency:      in.Currency,
		LegalEntity:   in.LegalEntity,
		PrevYearByRow: map[[2]int]float64{},
		CommentByRow:  map[[2]int]string{},
	}
	for _, r := range rows {
		key := [2]int{r.CodeCFO, r.CodePL}
		if r.Comment != "" {
			vin.CommentByRow[key] = r.Comment
		}
		// База сравнения E-18 — факт прошлого года той же строки: он уже лежит
		// рядом в форме, отдельный запрос к источнику не нужен.
		var prev float64
		for _, v := range r.Values {
			if v.Scenario == ExpScenarioFact && v.Amount != nil {
				prev += *v.Amount
			}
		}
		if prev != 0 {
			vin.PrevYearByRow[key] = prev
		}
	}
	if names, err := s.expenseNames(ctx); err == nil {
		vin.ExpenseByCodePL = names
	}

	issues := ValidateExpForm(vin)
	recon := ExpReconcile(ExpReconInput{Rows: rows, Registries: s.registriesFor(ctx, in)})

	blocking := ExpBlocking(issues)
	strict := s.cards != nil && s.cards.SubmitStrict()
	return ExpValidationReport{
		CardID:           c.ID,
		Blocking:         blocking,
		Warnings:         ExpWarnings(issues),
		Recon:            recon,
		CanSubmit:        !strict || (len(blocking) == 0 && len(ExpReconBlocking(recon)) == 0),
		Strict:           strict,
		ReconDiff:        ExpReconDiffSum(recon),
		UnclosedComments: ExpUnclosedComments(issues, vin.CommentByRow),
	}, nil
}

// registriesFor — строки реестров для контролей E-04…E-06. Недоступный реестр
// пропускает контроль, а не обнуляет его: «не проверено» и «сошлось» — разное.
func (s *ExpService) registriesFor(ctx context.Context, in ExpInstance) map[string][]ExpRegRow {
	if s.reg == nil {
		return nil
	}
	out := map[string][]ExpRegRow{}
	for _, kind := range ExpRegKinds() {
		rows, err := s.reg.RegRows(ctx, in.ID, kind)
		if err != nil {
			log.Printf("plans/exp: реестр %s не прочитан для контролей: %v", kind, err)
			continue
		}
		out[kind] = rows
	}
	return out
}

// expenseNames — наименования статей справочника для E-17.
func (s *ExpService) expenseNames(ctx context.Context) (map[int]string, error) {
	type namer interface {
		ExpenseNames(ctx context.Context) (map[int]string, error)
	}
	n, ok := s.repo.(namer)
	if !ok {
		return nil, nil
	}
	return n.ExpenseNames(ctx)
}

// expSubmitGate — гейт отправки формы 1.5.
type expSubmitGate struct{ svc *ExpService }

// NewExpSubmitGate — конструктор гейта.
func NewExpSubmitGate(svc *ExpService) SubmitGate { return &expSubmitGate{svc: svc} }

// CanSubmit — причины, по которым форму нельзя отправить. Пустой список — можно.
// Блокирует ли список отправку на самом деле, решает режим гейта (ответ 41).
func (g *expSubmitGate) CanSubmit(ctx context.Context, c Card) ([]string, error) {
	if g.svc == nil {
		return nil, nil
	}
	rep, err := g.svc.Validate(ctx, Principal{PlansAdmin: true}, c.ID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rep.Blocking)+4)
	for _, b := range rep.Blocking {
		out = append(out, b.Code+": "+b.Message)
	}
	out = append(out, ExpReconBlocking(rep.Recon)...)
	return out, nil
}

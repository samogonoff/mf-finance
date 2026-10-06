package plans

import (
	"context"
	"log"
)

// Согласование этапов периода с состоянием карточек форм.
//
// Проблема, которую это закрывает. В модуле три сущности процесса, и до сих
// пор они двигались НЕЗАВИСИМО:
//
//	этап (pl_stage_instance)  — календарь и сроки схемы ТЗ, двигался кнопками;
//	задание (pl_task)         — «кому заполнить», статус тоже вручную;
//	карточка формы (form_card)— фактический маршрут формы (заполнение → 1.2 → 1.3 → 1.4).
//
// В результате таймлайн мог показывать «этап 1.2 готово», когда формы ещё лежат
// в черновиках, — и наоборот. Именно на это наткнулся заказчик: он двигал этапы,
// считая, что двигает процесс, а карточки оставались на месте.
//
// Решение: этап 1.1–1.4 — это АГРЕГАТ состояния карточек, а не отдельная воля.
// Считается он здесь, чистой функцией, и применяется после каждого перехода
// карточки. Ручное движение остаётся только у этапов, которым карточки не
// соответствуют (производство 2.x, расходы 1.5, свод 1.6, ЮЛ 3, итог 4).
//
// Почему агрегат, а не запрет ручного движения: запрет оставил бы человеку два
// действия на один смысл («согласовать форму» и «закрыть этап»), и второе всё
// равно кто-нибудь нажал бы раньше первого.

// stageFormSteps — шаги маршрута форм в порядке прохождения. Совпадают с кодами
// этапов схемы, кроме «fin» (шаг финансового контролёра внутри формы; своего
// этапа у него нет — по ТЗ это часть 1.1/1.2).
var stageFormSteps = []string{"1.1", StepFinCode, "1.2", "1.3", "1.4"}

// StepFinCode — код шага «Финансовый контролёр» в маршруте формы. Своего этапа в
// схеме ТЗ у него нет: по ТЗ это часть 1.1/1.2, поэтому этап он не двигает, но
// право на него означает роль финансиста в форме.
const StepFinCode = "fin"

// stepOrder — позиция шага в маршруте формы; -1 для неизвестного шага.
func stepOrder(code string) int {
	for i, s := range stageFormSteps {
		if s == code {
			return i
		}
	}
	return -1
}

// cardPassedStep — карточка уже прошла этот шаг маршрута.
func cardPassedStep(c Card, step string) bool {
	// Утверждённая/опубликованная/архивная карточка прошла весь маршрут.
	switch c.Status {
	case CardApproved, CardPublished, CardPublishFailed, CardArchived:
		return true
	}
	si, ci := stepOrder(step), stepOrder(c.StepCode)
	if si < 0 || ci < 0 {
		return false
	}
	return ci > si
}

// expPeriodStage — этап схемы, за который отвечают карточки формы расходов ЦП.
const expPeriodStage = "1.5"

// formDrivesStage — двигают ли карточки формы этап периода с таким кодом.
//
// До появления формы расходов ЦП карточки были только у форм потока продаж, и
// «все карточки периода» совпадало с «карточки этапов 1.1–1.4». Формы расходов
// идут своим маршрутом с ТАКИМИ ЖЕ кодами шагов (1.1 → 1.2 → 1.4, но внутри
// своей формы), поэтому без разделения этап 1.1 периода начал бы ждать формы
// расходов — а они по схеме заполняются позже, на этапе 1.5.
func formDrivesStage(formCode, stageCode string) bool {
	if formCode == TemplateExp {
		return stageCode == expPeriodStage
	}
	return stepOrder(stageCode) >= 0
}

// cardClosedForStage — карточка формы уже «сделала» этот этап периода.
//
// У потока продаж этап закрывается прохождением одноимённого шага маршрута. У
// расходов ЦП шага с кодом «1.5» в маршруте нет: этап закрывает утверждение
// формы целиком.
func cardClosedForStage(c Card, stageCode string) bool {
	if c.FormCode == TemplateExp {
		switch c.Status {
		case CardApproved, CardPublished, CardPublishFailed, CardArchived:
			return true
		}
		return false
	}
	return cardPassedStep(c, stageCode)
}

// StageStatusFromCards — пересчёт статусов этапов по карточкам периода.
//
// Правила на один этап (только для тех, где есть соответствующие карточки):
//   - все карточки прошли шаг                        → completed
//   - есть карточка, возвращённая на этот шаг         → returned
//   - есть карточка, стоящая на этом шаге             → in_progress
//   - карточек нет вовсе                              → статус не меняем
//
// «returned» важнее «in_progress»: возврат — это событие, которое обязано быть
// видно на таймлайне, иначе о нём узнает только тот, кто откроет карточку.
func StageStatusFromCards(stages []StageState, cards []Card) []StageState {
	if len(cards) == 0 {
		return stages
	}
	out := make([]StageState, len(stages))
	copy(out, stages)

	for i := range out {
		step := out[i].Code
		if stepOrder(step) < 0 && step != expPeriodStage {
			continue // этап без карточек (2.x, 1.6, 3, 4) — движется вручную
		}
		var total, passed, onStep, returned int
		for _, c := range cards {
			// Этап агрегируется только из карточек СВОИХ форм: иначе этап 1.1
			// ждал бы расходы ЦП, а этап 1.5 — план продаж.
			if !formDrivesStage(c.FormCode, step) {
				continue
			}
			total++
			switch {
			case cardClosedForStage(c, step):
				passed++
			case c.FormCode == TemplateExp:
				// У расходов ЦП шага с кодом этапа в маршруте нет, поэтому
				// «в работе» и «возвращена» читаются по статусу формы.
				if c.Status == CardReturned {
					returned++
				} else if c.Status == CardOnApproval {
					onStep++
				}
			case (c.StepCode == step || step == "1.2" && c.StepCode == StepFinCode) && c.Status == CardReturned:
				returned++
			case c.StepCode == step || step == "1.2" && c.StepCode == StepFinCode:
				onStep++
			}
		}
		if total == 0 {
			continue
		}
		switch {
		case passed == total:
			out[i].Status = "completed"
		case returned > 0:
			out[i].Status = "returned"
		case onStep > 0 || passed > 0:
			out[i].Status = "in_progress"
		}
	}
	return out
}

// StageStore — доступ к этапам периода (реализует pgStore).
type StageStore interface {
	Stages(ctx context.Context, plID int64) ([]StageState, error)
	StagesSave(ctx context.Context, plID int64, stages []StageState) error
}

// WithStages подключает пересчёт этапов по карточкам.
func (s *CardService) WithStages(st StageStore) *CardService {
	s.stages = st
	return s
}

// syncStages — пересчитать этапы периода после перехода карточки.
//
// Ошибки не поднимаем наверх: переход карточки уже состоялся и откатывать его
// из-за таймлайна нельзя. Молчать тоже нельзя — рассинхрон таймлайна и карточек
// это ровно та проблема, которую метод и лечит, поэтому пишем в лог.
func (s *CardService) syncStages(ctx context.Context, plID int64) {
	if s.stages == nil || plID == 0 {
		return
	}
	stages, err := s.stages.Stages(ctx, plID)
	if err != nil || len(stages) == 0 {
		if err != nil {
			log.Printf("plans: этапы периода %d не прочитаны: %v", plID, err)
		}
		return
	}
	cards, err := s.cards.Cards(ctx, plID)
	if err != nil {
		log.Printf("plans: карточки периода %d не прочитаны: %v", plID, err)
		return
	}
	// Задания заполнения — тоже следствие состояния формы: «заполни форму»
	// выполнено, когда форма отправлена (task_card_sync.go).
	if s.fillTasks != nil {
		if _, err := s.fillTasks.SyncFillTasks(ctx, plID, cards); err != nil {
			log.Printf("plans: задания заполнения периода %d не синхронизированы: %v", plID, err)
		}
	}

	next := StageStatusFromCards(stages, cards)
	if !stagesChanged(stages, next) {
		return
	}
	if err := s.stages.StagesSave(ctx, plID, next); err != nil {
		log.Printf("plans: этапы периода %d не сохранены: %v", plID, err)
	}
}

// FillTaskSyncer — синхронизация заданий заполнения (реализует TaskStore).
type FillTaskSyncer interface {
	SyncFillTasks(ctx context.Context, plID int64, cards []Card) (int, error)
}

// WithFillTasks подключает синхронизацию заданий заполнения.
func (s *CardService) WithFillTasks(t FillTaskSyncer) *CardService {
	s.fillTasks = t
	return s
}

// stagesChanged — есть ли смысл писать в базу.
func stagesChanged(a, b []StageState) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i].Status != b[i].Status {
			return true
		}
	}
	return false
}

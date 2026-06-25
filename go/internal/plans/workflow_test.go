package plans

import "testing"

func findStage(stages []StageState, code string) *StageState {
	for i := range stages {
		if stages[i].Code == code {
			return &stages[i]
		}
	}
	return nil
}

func TestInitStages_AllStagesPendingWithDue(t *testing.T) {
	stages := initStages(2026, 6, "RU", CalendarSeed(), nil)
	if len(stages) != len(stageDefs()) {
		t.Fatalf("ожидалось %d этапов, got %d", len(stageDefs()), len(stages))
	}
	for _, s := range stages {
		if s.Status != "pending" {
			t.Errorf("этап %s должен быть pending, got %s", s.Code, s.Status)
		}
		if s.DueDate == "" {
			t.Errorf("этап %s без срока", s.Code)
		}
	}
}

func TestApplyAction_SubmitNoDeps(t *testing.T) {
	stages := initStages(2026, 6, "RU", CalendarSeed(), nil)
	out, err := applyStageAction(stages, "1.1", "submit", "")
	if err != nil {
		t.Fatalf("submit 1.1: %v", err)
	}
	if findStage(out, "1.1").Status != "completed" {
		t.Errorf("1.1 должен стать completed")
	}
}

func TestApplyAction_DependencyBlocks16Until24(t *testing.T) {
	stages := initStages(2026, 6, "RU", CalendarSeed(), nil)
	// 1.6 зависит от 1.5 и 2.4 (WF-DEP-01) — пока не завершены, submit нельзя.
	if _, err := applyStageAction(stages, "1.6", "submit", ""); err == nil {
		t.Error("ожидалась ошибка зависимости: 1.6 до 2.4/1.5")
	}
	// Завершаем цепочку продаж и производства.
	for _, code := range []string{"1.1", "1.2", "1.3", "1.4", "1.5", "2.1", "2.2", "2.3", "2.4"} {
		var err error
		stages, err = applyStageAction(stages, code, "submit", "")
		if err != nil {
			t.Fatalf("submit %s: %v", code, err)
		}
	}
	out, err := applyStageAction(stages, "1.6", "submit", "")
	if err != nil {
		t.Fatalf("1.6 после 2.4 должен пройти: %v", err)
	}
	if findStage(out, "1.6").Status != "completed" {
		t.Error("1.6 должен стать completed после зависимостей")
	}
}

func TestApplyAction_ReturnSetsTarget(t *testing.T) {
	stages := initStages(2026, 6, "RU", CalendarSeed(), nil)
	stages, _ = applyStageAction(stages, "1.1", "submit", "")
	out, err := applyStageAction(stages, "1.2", "return", "1.1")
	if err != nil {
		t.Fatalf("return: %v", err)
	}
	if findStage(out, "1.2").Status != "returned" {
		t.Error("1.2 должен стать returned")
	}
	if findStage(out, "1.1").Status != "in_progress" {
		t.Error("целевой этап 1.1 должен вернуться в in_progress")
	}
}

func TestApplyAction_UnknownStage(t *testing.T) {
	stages := initStages(2026, 6, "RU", CalendarSeed(), nil)
	if _, err := applyStageAction(stages, "9.9", "submit", ""); err == nil {
		t.Error("ожидалась ошибка: неизвестный этап")
	}
}

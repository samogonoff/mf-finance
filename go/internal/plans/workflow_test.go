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

// 1.6 ждёт только поток продаж (1.5): производство (2.4) пока не блокирует — его нет в кабинете.
func TestApplyAction_Stage16WaitsForSalesOnly(t *testing.T) {
	stages := initStages(2026, 6, "RU", CalendarSeed(), nil)
	if _, err := applyStageAction(stages, "1.6", "submit", ""); err == nil {
		t.Error("ожидалась ошибка зависимости: 1.6 до завершения 1.5")
	}
	// Завершаем только поток продаж; производство (2.1–2.4) не трогаем.
	for _, code := range []string{"1.1", "1.2", "1.3", "1.4", "1.5"} {
		var err error
		stages, err = applyStageAction(stages, code, "submit", "")
		if err != nil {
			t.Fatalf("submit %s: %v", code, err)
		}
	}
	if findStage(stages, "2.4").Status == "completed" {
		t.Fatal("контроль теста: производство не завершалось")
	}
	out, err := applyStageAction(stages, "1.6", "submit", "")
	if err != nil {
		t.Fatalf("1.6 после 1.5 должен проходить без 2.4: %v", err)
	}
	if findStage(out, "1.6").Status != "completed" {
		t.Error("1.6 должен стать completed")
	}
	// Этап 3 ждёт 1.6 (WF-DEP-02).
	if _, err := applyStageAction(stages, "3", "submit", ""); err == nil {
		t.Error("этап 3 до завершения 1.6 нельзя")
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

func TestApplyAction_ReturnResetsIntermediateApproval(t *testing.T) {
	stages := initStages(2026, 6, "RU", CalendarSeed(), nil)
	for i := range stages {
		switch stages[i].Code {
		case "1.5", "1.6", "3", "4", "2.4":
			stages[i].Status = "completed"
		}
	}
	out, err := applyStageAction(stages, "4", "return", "1.6")
	if err != nil {
		t.Fatal(err)
	}
	for code, want := range map[string]string{
		"1.5": "completed", "1.6": "in_progress", "3": "pending", "4": "returned", "2.4": "completed",
	} {
		if got := findStage(out, code).Status; got != want {
			t.Errorf("после возврата на 1.6 этап %s: %q, ожидали %q", code, got, want)
		}
	}
	if _, err := applyStageAction(out, "3", "approve", ""); err == nil {
		t.Error("этап 3 нельзя согласовать до повторного завершения 1.6")
	}
}

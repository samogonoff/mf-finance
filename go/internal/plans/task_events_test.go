package plans

import "testing"

// Срок передачи разбирается из нескольких удобных форматов, а мусор отвергается
// с внятным текстом: поле заполняет человек, а не система.
func TestParseTaskDue(t *testing.T) {
	for _, raw := range []string{"2026-06-02", "2026-06-02T15:04", "2026-06-02T15:04:05Z"} {
		got, err := parseTaskDue(raw)
		if err != nil || got == nil {
			t.Fatalf("%q: получено (%v, %v)", raw, got, err)
		}
		if got.Year() != 2026 || got.Month() != 6 || got.Day() != 2 {
			t.Fatalf("%q разобрано как %s", raw, got)
		}
	}
	if got, err := parseTaskDue("  "); err != nil || got != nil {
		t.Fatalf("пустой срок — это отсутствие срока, получено (%v, %v)", got, err)
	}
	if _, err := parseTaskDue("во вторник"); err == nil {
		t.Fatal("неразбираемый срок должен отвергаться")
	}
}

// Действия журнала переводятся на человеческий язык: списки заданий читают
// исполнители, и «delegate» им ничего не говорит.
func TestTaskActionLabel(t *testing.T) {
	cases := map[string]string{
		"start":    "взял(а) в работу",
		"delegate": "передал(а) в работу",
		"submit":   "сдал(а)",
		"accept":   "принял(а)",
		"return":   "вернул(а) на доработку",
		"reopen":   "переоткрыл(а)",
	}
	for action, want := range cases {
		if got := TaskActionLabel(action); got != want {
			t.Errorf("%s → %q, ожидалось %q", action, got, want)
		}
	}
	// Незнакомое действие показываем как есть, а не прячем.
	if got := TaskActionLabel("custom"); got != "custom" {
		t.Errorf("незнакомое действие должно отдаваться как есть, получено %q", got)
	}
}

package bugtracker

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func strp(s string) *string { return &s }
func i64p(v int64) *int64   { return &v }

func TestValidatePatch_ResolvedRequiresSource(t *testing.T) {
	cur := &Report{Status: StatusNew}
	err := validatePatch(cur, PatchInput{Status: strp(StatusResolved)})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("«Решён» без причины должен давать ValidationError, got %v", err)
	}
}

func TestValidatePatch_ResolvedWithSourceInBody(t *testing.T) {
	cur := &Report{Status: StatusNew}
	if err := validatePatch(cur, PatchInput{Status: strp(StatusResolved), SourceID: i64p(3)}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

// Причину проставила автоматика вместе с MR — закрыть можно без повторной передачи.
func TestValidatePatch_ResolvedWithExistingSource(t *testing.T) {
	cur := &Report{Status: StatusInProgress, SourceID: i64p(3)}
	if err := validatePatch(cur, PatchInput{Status: strp(StatusResolved)}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}

// Снять причину у решённого бага нельзя — иначе «Решён» без источника.
func TestValidatePatch_CannotClearSourceOfResolved(t *testing.T) {
	cur := &Report{Status: StatusResolved, SourceID: i64p(3)}
	if err := validatePatch(cur, PatchInput{SourceID: i64p(0)}); err == nil {
		t.Fatalf("снятие причины у resolved должно отклоняться")
	}
}

func TestValidatePatch_InvalidStatus(t *testing.T) {
	if err := validatePatch(&Report{Status: StatusNew}, PatchInput{Status: strp("done")}); err == nil {
		t.Fatalf("неизвестный статус должен отклоняться")
	}
}

func TestValidatePatch_OtherStatusesNeedNoSource(t *testing.T) {
	for _, st := range []string{StatusNew, StatusInProgress, StatusDuplicate, StatusRejected} {
		if err := validatePatch(&Report{Status: StatusNew}, PatchInput{Status: strp(st)}); err != nil {
			t.Errorf("%s: unexpected %v", st, err)
		}
	}
}

func TestStatusSQL_DropsClaimOutsideInProgress(t *testing.T) {
	sql := statusSQL("$2::text")
	for _, want := range []string{"claimed_by  = CASE WHEN $2::text = 'in_progress'", "resolved_at = CASE WHEN $2::text = 'resolved'"} {
		if !strings.Contains(sql, want) {
			t.Errorf("statusSQL должен содержать %q:\n%s", want, sql)
		}
	}
}

func TestNormalizeCreate_CapsAndDefaults(t *testing.T) {
	logs := make([]any, 70)
	for i := range logs {
		logs[i] = i
	}
	in, err := normalizeCreate(CreateInput{
		Section:     "unknown",
		Type:        "weird",
		Title:       "  Заголовок  ",
		Description: strings.Repeat("я", maxDescriptionRunes+100),
		Route:       map[string]any{"url": "http://finance.local/plans"},
		ConsoleLogs: logs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.Section != "other" || in.Type != TypeBug {
		t.Errorf("section/type должны уйти в дефолты, got %q/%q", in.Section, in.Type)
	}
	if in.Title != "Заголовок" {
		t.Errorf("title должен обрезаться по краям, got %q", in.Title)
	}
	if n := utf8.RuneCountInString(in.Description); n != maxDescriptionRunes+1 { // + «…»
		t.Errorf("description должен резаться до %d символов, got %d", maxDescriptionRunes, n)
	}
	if !utf8.ValidString(in.Description) {
		t.Errorf("обрезка не должна ломать UTF-8")
	}
	if in.URL != "http://finance.local/plans" {
		t.Errorf("url должен браться из route.url, got %q", in.URL)
	}
	if len(in.ConsoleLogs) != maxLogEntries || in.ConsoleLogs[0] != 20 {
		t.Errorf("из логов остаются последние %d записей, got len=%d first=%v", maxLogEntries, len(in.ConsoleLogs), in.ConsoleLogs[0])
	}
}

func TestNormalizeCreate_TitleRequired(t *testing.T) {
	if _, err := normalizeCreate(CreateInput{Title: "   "}); err == nil {
		t.Fatalf("пустой title должен отклоняться")
	}
}

func TestNormalizeCreate_PlansSectionAllowed(t *testing.T) {
	in, _ := normalizeCreate(CreateInput{Section: "plans", Title: "x"})
	if in.Section != "plans" {
		t.Errorf("раздел plans должен проходить белый список, got %q", in.Section)
	}
}

// Явный url важнее route.url — в сигнатуре и в поле.
func TestComputeSignature_PrefersExplicitURL(t *testing.T) {
	a := baseInput()
	b := baseInput()
	b.URL = "http://finance.local/other"
	if computeSignature(a) == computeSignature(b) {
		t.Errorf("явный url должен участвовать в сигнатуре")
	}
}

func TestNormalizeAuto_Defaults(t *testing.T) {
	in, err := normalizeAuto(AutoCreateInput{Description: "Публикация падает\nподробности в ELK"})
	if err != nil {
		t.Fatal(err)
	}
	if in.Section != SectionOps || in.Type != TypeBug || in.By != defaultAgent {
		t.Errorf("дефолты: section=%q type=%q by=%q", in.Section, in.Type, in.By)
	}
	if in.Title != "Публикация падает" {
		t.Errorf("заголовок — первая строка описания, got %q", in.Title)
	}
	if len(in.Signature) != 64 {
		t.Errorf("без своей сигнатуры вычисляется SHA256, got %q", in.Signature)
	}
}

func TestNormalizeAuto_KeepsOwnSignature(t *testing.T) {
	in, _ := normalizeAuto(AutoCreateInput{Description: "x", Signature: " elk:plans_publish:timeout "})
	if in.Signature != "elk:plans_publish:timeout" {
		t.Errorf("сигнатура автоматики должна сохраняться, got %q", in.Signature)
	}
}

func TestNormalizeAuto_Validation(t *testing.T) {
	cases := []AutoCreateInput{
		{Description: "  "},
		{Description: "x", Section: "nope"},
		{Description: "x", Type: "nope"},
	}
	for i, c := range cases {
		if _, err := normalizeAuto(c); err == nil {
			t.Errorf("case %d: ожидалась ошибка валидации", i)
		}
	}
}

func TestNewReportNotification(t *testing.T) {
	n := newReportNotification(&Report{
		ID: 42, Section: "plans", Type: TypeData, Title: "Не сходится свод",
		URL:  "http://finance.local/plans/tasks/7",
		User: &UserShort{ID: 5, Name: "Иван Петров", Email: "ivan@x"},
	})
	if n.Title != "Новая заявка #42 из багтрекера (Данные)" {
		t.Errorf("title: %q", n.Title)
	}
	for _, want := range []string{"Раздел: plans", "Автор: Иван Петров", "URL: http://finance.local/plans/tasks/7", "Не сходится свод"} {
		if !strings.Contains(n.Message, want) {
			t.Errorf("message должен содержать %q:\n%s", want, n.Message)
		}
	}
	if n.Data["url"] != "/admin/bugtracker?id=42" {
		t.Errorf("ссылка должна открывать карточку репорта, got %v", n.Data["url"])
	}
}

func TestMRNotificationText_NoDeployCommand(t *testing.T) {
	url := "https://git/mr/12"
	txt := mrNotificationText(&Report{Section: "plans", Type: TypeBug, Title: "t", MRURL: &url}, "поправил расчёт")
	if !strings.Contains(txt, "MR: "+url) || !strings.Contains(txt, "Что сделано: поправил расчёт") {
		t.Errorf("unexpected text:\n%s", txt)
	}
	if strings.Contains(txt, "/deploy") {
		t.Errorf("команде выката не место в уведомлении (дублируется в Б24)")
	}
}

func TestAgentName(t *testing.T) {
	if agentName("  ") != defaultAgent {
		t.Errorf("пустое имя → %q", defaultAgent)
	}
	if n := utf8.RuneCountInString(agentName(strings.Repeat("a", 100))); n > maxAgentRunes+1 {
		t.Errorf("имя агента должно обрезаться, got %d", n)
	}
}

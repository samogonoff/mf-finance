package plans

import "testing"

// Формат автора (правка ТЗ 02.09.2026 Розница §25): в базу — доменный логин, в
// интерфейс — ФИО без домена и без логина.

func TestAuthorForDB_KeepsDomainFormat(t *testing.T) {
	if got := AuthorForDB("n.gubarenko"); got != `PRAKTIK\n.gubarenko` {
		t.Fatalf("got %q, want PRAKTIK\\n.gubarenko", got)
	}
	// Логин уже с доменом — второй раз домен не добавляется, иначе получится
	// PRAKTIK\PRAKTIK\… и строка не сойдётся с историческими.
	if got := AuthorForDB(`PRAKTIK\n.gubarenko`); got != `PRAKTIK\n.gubarenko` {
		t.Fatalf("повторное добавление домена: got %q", got)
	}
	if got := AuthorForDB("  "); got != "" {
		t.Fatalf("пустой логин не должен превращаться в домен: got %q", got)
	}
}

func TestAuthorDisplay_HidesDomainAndLogin(t *testing.T) {
	// Есть ФИО — показываем его.
	if got := AuthorDisplay("Губаренко Н.", `PRAKTIK\n.gubarenko`); got != "Губаренко Н." {
		t.Fatalf("got %q, want ФИО", got)
	}
	// ФИО нет — показываем логин БЕЗ домена (домен в интерфейсе бессмыслен).
	if got := AuthorDisplay("", `PRAKTIK\n.gubarenko`); got != "n.gubarenko" {
		t.Fatalf("got %q, want n.gubarenko", got)
	}
	if got := AuthorDisplay("", "n.gubarenko"); got != "n.gubarenko" {
		t.Fatalf("логин без домена должен остаться собой: got %q", got)
	}
}

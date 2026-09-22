package plans

import "strings"

// Формат автора строки плана (правка ТЗ 02.09.2026 Розница §25).
//
// Правка: «в базу автор пишется доменным логином вида PRAKTIK\n.gubarenko —
// формат сохраняем, иначе новые строки не сойдутся со старыми. В интерфейсе домен
// не показываем: выводим ФИО из авторизации, без PRAKTIK\ и без логина.
// Преобразование только на выводе».
//
// Отсюда два правила, и они не симметричны:
//   - НА ЗАПИСЬ формат источника сохраняется как есть. Даже если домен кажется
//     лишним, его отсутствие делает наши строки непохожими на исторические, и
//     сверка «кто автор» перестаёт работать по всей таблице.
//   - НА ВЫВОД показывается ФИО пользователя из авторизации. Логин не
//     показывается вообще: в интерфейсе он ничего не добавляет, а домен читается
//     как техническая деталь.

// AuthorDomain — домен, под которым пишутся авторы в исторических строках.
const AuthorDomain = "PRAKTIK"

// AuthorForDB — значение для записи в базу: домен + логин. Если логин уже пришёл
// с доменом, второй раз он не добавляется.
func AuthorForDB(login string) string {
	l := strings.TrimSpace(login)
	if l == "" {
		return ""
	}
	if strings.Contains(l, `\`) {
		return l
	}
	return AuthorDomain + `\` + l
}

// AuthorDisplay — что показать в интерфейсе: ФИО из авторизации, а если его нет —
// логин без домена. Пустое ФИО подменять доменным логином нельзя: пользователь
// увидел бы техническую строку там, где ждёт человека.
func AuthorDisplay(fullName, dbAuthor string) string {
	if n := strings.TrimSpace(fullName); n != "" {
		return n
	}
	return AuthorLoginWithoutDomain(dbAuthor)
}

// AuthorLoginWithoutDomain — логин без доменного префикса.
func AuthorLoginWithoutDomain(dbAuthor string) string {
	a := strings.TrimSpace(dbAuthor)
	if i := strings.LastIndex(a, `\`); i >= 0 {
		return strings.TrimSpace(a[i+1:])
	}
	return a
}

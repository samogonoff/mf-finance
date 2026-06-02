package debt

import (
	"regexp"
	"strings"
	"time"
)

// ParsedDoc — что мы вытащили из текстовых полей проводки
// (Objects.Name, Mapping или TransDescription) для drill-down.
type ParsedDoc struct {
	Kind   string    // тип документа: «Реализация (акт, накладная, УПД)», «Поступление», «Входящий документ»
	Number string    // номер документа: «ТДБП-003950», «0000338/5028»
	Date   time.Time // дата документа из текста (если есть), иначе zero
	Source string    // откуда: "objects", "mapping", "trans_description", "fallback"
}

// reObjects — формат `[FinDWH].[dbo].[Objects].Name` для документов 1С:
//   «Реализация (акт, накладная, УПД) ТДБП-003950 от 15.04.2026 10:00:00»
// kind — всё до последнего «<номер> от <дата>», num — последний токен перед «от», date.
var reObjects = regexp.MustCompile(
	`^(?P<kind>.+?)\s+(?P<num>\S+)\s+от\s+(?P<date>\d{2}\.\d{2}\.\d{4}(?:\s+\d{1,2}:\d{2}:\d{2})?)\s*$`,
)

// reMappingDoc — формат `Premaster1C.Mapping`:
//   «Реализация (акт, накладная, УПД) ТДБП-003950 от 15.04.2026 10:00:00 (документ), …»
// Хвост `(документ), <статья> (субконто)` отсекается.
var reMappingDoc = regexp.MustCompile(
	`^(?P<kind>.+?)\s+(?P<num>\S+)\s+от\s+(?P<date>\d{2}\.\d{2}\.\d{4}(?:\s+\d{1,2}:\d{2}:\d{2})?)\s+\(документ\)`,
)

// reIncoming — `Premaster1C.TransDescription` для входящих документов:
//   «…по вх.д. 0000338/5028 от 01.04.2026»
// Тип документа здесь не указан явно — отдаём фиксированный «Входящий документ».
var reIncoming = regexp.MustCompile(
	`по\s+вх\.д\.\s+(?P<num>\S+)\s+от\s+(?P<date>\d{2}\.\d{2}\.\d{4})`,
)

// dateLayouts — варианты записи даты в Mapping/Objects (с временем и без).
var dateLayouts = []string{
	"02.01.2006 15:04:05",
	"02.01.2006 15:04",
	"02.01.2006",
}

func parseDateRU(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, l := range dateLayouts {
		if t, err := time.ParseInLocation(l, s, time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}

// ParseObjectsName — первый источник: имя документа из [FinDWH].[dbo].[Objects].
// Самое чистое — нет хвоста, нет шума.
func ParseObjectsName(s string) (ParsedDoc, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ParsedDoc{}, false
	}
	m := reObjects.FindStringSubmatch(s)
	if m == nil {
		return ParsedDoc{}, false
	}
	return ParsedDoc{
		Kind:   strings.TrimSpace(m[1]),
		Number: strings.TrimSpace(m[2]),
		Date:   parseDateRU(m[3]),
		Source: "objects",
	}, true
}

// ParseMapping — второй источник: Premaster.Mapping. Используется когда
// Objects-резолвер ничего не вернул (например, документ удалён из 1С).
func ParseMapping(s string) (ParsedDoc, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ParsedDoc{}, false
	}
	m := reMappingDoc.FindStringSubmatch(s)
	if m == nil {
		return ParsedDoc{}, false
	}
	return ParsedDoc{
		Kind:   strings.TrimSpace(m[1]),
		Number: strings.TrimSpace(m[2]),
		Date:   parseDateRU(m[3]),
		Source: "mapping",
	}, true
}

// ParseTransDescription — третий источник: для входящих документов,
// у которых в Mapping нет шаблона «… (документ)».
func ParseTransDescription(s string) (ParsedDoc, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return ParsedDoc{}, false
	}
	m := reIncoming.FindStringSubmatch(s)
	if m == nil {
		return ParsedDoc{}, false
	}
	return ParsedDoc{
		Kind:   "Входящий документ",
		Number: strings.TrimSpace(m[1]),
		Date:   parseDateRU(m[2]),
		Source: "trans_description",
	}, true
}

// ResolveDoc применяет источники по приоритету: Objects → Mapping → TransDescription.
// Если ничего не сработало — возвращает ParsedDoc{Source: "fallback"} с пустыми полями.
// Вызывающий код должен подставить DocID как номер в случае fallback.
func ResolveDoc(objectsName, mapping, transDescription string) ParsedDoc {
	if p, ok := ParseObjectsName(objectsName); ok {
		return p
	}
	if p, ok := ParseMapping(mapping); ok {
		return p
	}
	if p, ok := ParseTransDescription(transDescription); ok {
		return p
	}
	return ParsedDoc{Source: "fallback"}
}

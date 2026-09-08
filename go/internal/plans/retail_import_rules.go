package plans

import "strings"

// Правила приёма строк справочника магазинов (правки 08.09.2026, пункты 34–35).
//
// Источник отдаёт не только магазины: рядом с ними лежат строки-ИТОГИ по стране и
// по LFL-статусу. Их видно только по подписи — «Розница РБ Итог», «LFL Итог», —
// а CodeCFO у них заполнен, поэтому прежний фильтр (числовой код + состав
// розницы) их пропускал. Каждый магазин попадал в сумму дважды: проверка на
// числах показала май 2026 по РБ 68 467 424 вместо 34 233 712, ровно вдвое.
//
// Контроль ожидаемого результата после отсечения (правка п. 34):
// Розница РБ 192, Розница РФ 74, Розница закрыты 55, Розница KZ 45, Розница UZ 6.

// retailTotalSuffix — подпись строки-итога. Сравнение по суффиксу, а не по
// вхождению: «Итоговый» в названии магазина не должен выбрасывать строку.
const retailTotalSuffix = "итог"

// isRetailTotalRow — строка является итогом группы, а не магазином.
func isRetailTotalRow(st RetailStore) bool {
	return endsWithTotal(st.Country) || endsWithTotal(st.LFLStatus)
}

func endsWithTotal(v string) bool {
	s := strings.ToLower(strings.TrimSpace(v))
	return strings.HasSuffix(s, retailTotalSuffix)
}

// retailStoreCountry — страна магазина.
//
// У группы «Розница закрыты» колонка страны несёт НАЗВАНИЕ ГРУППЫ, а не страну
// (правка п. 35), поэтому её берём по коду ЦФО из справочника. cfoCountry — карта
// «код ЦФО → страна» (dir_cfo); нет записи — оставляем как есть, чтобы строка не
// потеряла страну совсем.
func retailStoreCountry(st RetailStore, cfoCountry map[int]string) string {
	if isClosedGroup(st.GroupCFO1) || isClosedGroup(st.Country) {
		if c := cfoCountry[st.CodeCFO]; c != "" {
			return c
		}
	}
	return st.Country
}

// retailStoreLFL — LFL-статус магазина: строкам группы «закрыты» он ставится
// принудительно (правка п. 35), иначе берётся нормализованный статус источника.
func retailStoreLFL(st RetailStore) string {
	if isClosedGroup(st.GroupCFO1) || isClosedGroup(st.Country) {
		return LFLClosed
	}
	return st.LFLStatus
}

// isClosedGroup — подпись группы закрытых магазинов («Розница закрыты»).
func isClosedGroup(v string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(v)), "закрыт")
}

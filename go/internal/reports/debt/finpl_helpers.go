package debt

import "time"

// Хелперы источника DEBT_BACKEND=finpl (каноническая ОПУ-витрина Table_Fin_PL).
// Витрина месячная (поле Month = первое число месяца), поэтому период отчёта
// округляется к началу месяца, а нижняя граница клампится к DEBT_FINPL_MIN_MONTH
// (раньше неё данных в Table_Fin_PL нет). См. SPEC §3 (решения #4/#7).

// vgoFinPLClause — ВГО-условие для finpl-отчёта. В Table_Fin_PL признак ВГО уже
// посчитан апстримом (Update_Table_Fin_PL: [ВГО] = IIF(ICO=0,0,1)) и шире нашего
// хардкода OurINNs — включает внутригрупповые ЮЛ сверх 15 (Дримдом/DR2/GP).
// Поэтому фильтр безусловный, без списка ИНН (ср. vgoMSSQLClause/vgoCHClause).
func vgoFinPLClause() string {
	return " AND [ВГО] = 1"
}

// monthStart — первое число календарного месяца t (UTC, без времени).
func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// parseFinPLMinMonth парсит DEBT_FINPL_MIN_MONTH (YYYY-MM-DD) в начало месяца.
// Пустая/битая строка → дефолт 2025-01-01 (нижняя граница данных Table_Fin_PL).
func parseFinPLMinMonth(s string) time.Time {
	def := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if s == "" {
		return def
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return def
	}
	return monthStart(t)
}

// finPLMonthFrom — нижняя граница периода: начало месяца dateFrom, но не раньше
// minMonth (клампинг — раньше minMonth в Table_Fin_PL пусто).
func finPLMonthFrom(dateFrom, minMonth time.Time) time.Time {
	from := monthStart(dateFrom)
	if from.Before(minMonth) {
		return monthStart(minMonth)
	}
	return from
}

// finPLMonthTo — верхняя граница: начало месяца dateTo (Month — первое число,
// поэтому фильтр Month <= finPLMonthTo включает весь месяц dateTo).
func finPLMonthTo(dateTo time.Time) time.Time {
	return monthStart(dateTo)
}

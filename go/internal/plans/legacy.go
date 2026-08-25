package plans

import "net/http"

// LegacyGate — заморозка устаревшей ветки API. Возвращает middleware: при
// enabled=false ручка отвечает 410 Gone с указанием актуального адреса, при
// true — работает как раньше (аварийный откат через PLANS_LEGACY_MP_API=1).
//
// Зачем: ветка /api/plans/mp/{form,compute,copy,formula,export,import} — первая
// генерация формы МП (2 editable-строки 1046/8006, свой движок calc.go/eval.go),
// фронтом больше не используется. Держать две живые генерации формы при переходе
// на инверсию расчёта (ТЗ МП §3.1) — гарантированное расхождение чисел.
func LegacyGate(enabled bool) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		if enabled {
			return next
		}
		return func(w http.ResponseWriter, r *http.Request) {
			writeErr(w, http.StatusGone,
				"ручка заморожена: актуальная форма МП — /api/plans/tasks/{taskId}/mp-form "+
					"(аварийный откат: PLANS_LEGACY_MP_API=1)")
		}
	}
}

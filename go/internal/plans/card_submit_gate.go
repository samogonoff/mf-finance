package plans

import (
	"context"
	"log"
	"strings"
)

// Гейт отправки формы на согласование.
//
// Проблема, которую он закрывает: валидации формы (V-01…V-11 у розницы,
// МП-01…МП-11 у маркетплейсов) считались и показывались в интерфейсе — кнопка
// «Отправить» гасла, — но САМ ПЕРЕХОД карточки их не проверял. То есть отправить
// незаполненную форму можно было запросом мимо интерфейса, а также в любой
// ситуации, где фронт не успел пересчитать валидации. На согласование уезжала
// форма, которую согласующий обязан вернуть, и процесс делал лишний круг.
//
// Гейт применяется ТОЛЬКО к действию submit (переход с шага заполнения). Дальше
// по маршруту он не нужен: значения уже заморожены, а согласующий решает не
// формальной проверкой, а по существу.
//
// Провайдер гейта опционален: форма без него ведёт себя как раньше. Это важно
// для форм, у которых валидаций пока нет (производство, затраты ЦЗ) — иначе
// подключение оболочки процесса к ним потребовало бы сразу писать валидации.

// SubmitGate — «можно ли отправлять форму на согласование».
type SubmitGate interface {
	// CanSubmit возвращает список ПРИЧИН, по которым отправка невозможна.
	// Пустой список — можно отправлять. Ошибка means «проверить не удалось».
	CanSubmit(ctx context.Context, c Card) ([]string, error)
}

// WithSubmitGate подключает проверку отправки для формы.
func (s *CardService) WithSubmitGate(formCode string, g SubmitGate) *CardService {
	if s.gates == nil {
		s.gates = map[string]SubmitGate{}
	}
	s.gates[formCode] = g
	return s
}

// checkSubmitGate — проверка перед отправкой на согласование.
//
// Недоступность проверки (источник факта отвалился, сверки не посчитались) НЕ
// блокирует отправку: иначе процесс встал бы из-за VPN, а не из-за данных. Такой
// случай пишем в лог — он должен быть виден, но не должен останавливать людей.
func (s *CardService) checkSubmitGate(ctx context.Context, c Card) error {
	g, ok := s.gates[c.FormCode]
	if !ok || g == nil {
		return nil
	}
	reasons, err := g.CanSubmit(ctx, c)
	if err != nil {
		log.Printf("plans: проверка отправки формы %s/%s не выполнена: %v", c.FormCode, c.ScopeKey, err)
		return nil
	}
	if len(reasons) == 0 {
		return nil
	}
	return &SubmitBlockedError{Reasons: reasons}
}

// SubmitBlockedError — отправка невозможна из-за блокирующих замечаний формы.
// Отдельный тип, чтобы обработчик мог вернуть перечень причин, а не одну строку:
// человеку нужно знать, что именно исправить.
type SubmitBlockedError struct{ Reasons []string }

func (e *SubmitBlockedError) Error() string {
	const limit = 5
	shown := e.Reasons
	suffix := ""
	if len(shown) > limit {
		shown, suffix = shown[:limit], "…"
	}
	return "форма не проходит обязательные проверки: " + strings.Join(shown, "; ") + suffix
}

// --- Реализация для формы «Розница» ---

// retailSubmitGate — гейт на отчёте валидаций розницы.
type retailSubmitGate struct{ svc *RetailService }

// NewRetailSubmitGate — конструктор.
func NewRetailSubmitGate(svc *RetailService) SubmitGate { return &retailSubmitGate{svc: svc} }

func (g *retailSubmitGate) CanSubmit(ctx context.Context, c Card) ([]string, error) {
	if g.svc == nil {
		return nil, nil
	}
	// Проверку делаем ОТ ИМЕНИ АДМИНА: отчёт считается по всем строкам экземпляра,
	// а не по видимым отправителю. Иначе РМ с узким срезом «проходил» гейт,
	// потому что чужие незаполненные магазины ему не видны.
	rep, err := g.svc.Validate(ctx, Principal{PlansAdmin: true}, c.ID)
	if err != nil {
		return nil, err
	}
	if rep.CanSubmit {
		return nil, nil
	}
	out := make([]string, 0, len(rep.Blocking))
	for _, b := range rep.Blocking {
		out = append(out, issueText(b))
	}
	return out, nil
}

// issueText — короткая формулировка замечания для сообщения об отказе.
func issueText(v ValidationIssue) string {
	msg := strings.TrimSpace(v.Message)
	if v.Code == "" {
		return msg
	}
	if msg == "" {
		return v.Code
	}
	return v.Code + ": " + msg
}

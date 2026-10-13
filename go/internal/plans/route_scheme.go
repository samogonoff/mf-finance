package plans

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Схема согласования «как её увидит согласующий» (ТЗ доработок кабинета, п. 4.3).
//
// «Показать действующую схему согласования розницы и МП в виде, в котором её
// увидит согласующий, до запуска. Не по документам, а по системе». Поэтому
// схема собирается из тех же данных, по которым карточка реально пойдёт:
// включённые шаги plans_form_route (выключенные не показываются — по ним форма
// не пройдёт) и исполнители, которых определит тот же StepOwnerResolver, что
// используется при переходах. Шаг согласования без исполнителя помечается
// пробелом: на нём карточка остановится с ошибкой «не назначен ответственный»
// (это и есть «пробелы» п. 4.2 — их видно до запуска, а не на первом переходе).

// RouteSchemeStep — шаг маршрута карточки.
type RouteSchemeStep struct {
	StepCode       string `json:"step_code"`
	StepName       string `json:"step_name"`
	Kind           string `json:"kind"`
	Responsible    string `json:"responsible"` // описание из настройки маршрута
	OwnerUserID    int64  `json:"owner_user_id,omitempty"`
	OwnerName      string `json:"owner_name,omitempty"`
	Gap            bool   `json:"gap"`     // согласующего шага нет — карточка здесь встанет
	Current        bool   `json:"current"` // карточка сейчас на этом шаге
	SkipIfSameUser bool   `json:"skip_if_same_user"`
	DueRD          int    `json:"due_rd"`
}

// RouteSchemeCard — маршрут одной карточки.
type RouteSchemeCard struct {
	CardID   int64             `json:"card_id"`
	FormCode string            `json:"form_code"`
	ScopeKey string            `json:"scope_key"`
	Title    string            `json:"title"`
	Status   string            `json:"status"`
	Steps    []RouteSchemeStep `json:"steps"`
	Gaps     int               `json:"gaps"`
	Error    string            `json:"error,omitempty"`
}

// RouteSchemeHandler — GET /api/plans/period/{plId}/route-scheme.
type RouteSchemeHandler struct {
	cards  *CardService
	owners StepOwnerResolver
	pool   *pgxpool.Pool
	prin   PrincipalFunc
	scope  ScopeStore
}

// NewRouteSchemeHandler — конструктор.
func NewRouteSchemeHandler(cards *CardService, owners StepOwnerResolver, pool *pgxpool.Pool, scope ScopeStore, prin PrincipalFunc) *RouteSchemeHandler {
	return &RouteSchemeHandler{cards: cards, owners: owners, pool: pool, scope: scope, prin: prin}
}

// buildRouteScheme — шаги карточки из маршрута и исполнителей (чистая функция).
func buildRouteScheme(c Card, route []RouteStep, owners map[string]int64, names map[int64]string) RouteSchemeCard {
	out := RouteSchemeCard{CardID: c.ID, FormCode: c.FormCode, ScopeKey: c.ScopeKey, Title: c.Title,
		Status: CardStatusLabel(c.Status, c.StepCode)}
	steps := append([]RouteStep(nil), route...)
	sort.SliceStable(steps, func(i, j int) bool { return steps[i].SortOrder < steps[j].SortOrder })
	for _, st := range steps {
		if !st.Enabled {
			continue
		}
		s := RouteSchemeStep{StepCode: st.StepCode, StepName: st.StepName, Kind: st.Kind,
			Responsible: st.Responsible, SkipIfSameUser: st.SkipIfSameUser, DueRD: st.DueRD,
			Current: c.StepCode == st.StepCode}
		if uid := owners[st.StepCode]; uid != 0 {
			s.OwnerUserID, s.OwnerName = uid, names[uid]
		} else if st.Kind != StepFill {
			// Шаг заполнения ведёт исполнитель задания — его отсутствие видно в
			// процессе. Согласующий же шаг без исполнителя останавливает переход.
			s.Gap = true
			out.Gaps++
		}
		out.Steps = append(out.Steps, s)
	}
	return out
}

func (h *RouteSchemeHandler) userNames(ctx context.Context, ids []int64) map[int64]string {
	out := map[int64]string{}
	if h.pool == nil || len(ids) == 0 {
		return out
	}
	rows, err := h.pool.Query(ctx, `SELECT id, TRIM(last_name || ' ' || name) FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) == nil {
			out[id] = strings.TrimSpace(name)
		}
	}
	return out
}

// Get — схема согласования карточек розницы и МП периода (видимых пользователю).
func (h *RouteSchemeHandler) Get(w http.ResponseWriter, r *http.Request) {
	plID, ok := pathID(w, r, "plId")
	if !ok {
		return
	}
	ctx := r.Context()
	var p Principal
	if h.prin != nil {
		p, _ = h.prin(r)
	}
	filter := NewScopeFilter(p.PlansAdmin, nil)
	if h.scope != nil {
		f, err := scopeFilterFor(ctx, h.scope, p)
		if err != nil {
			writeErr(w, http.StatusForbidden, err.Error())
			return
		}
		filter = f
	}
	cards, err := h.cards.Cards(ctx, plID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sort.SliceStable(cards, func(i, j int) bool {
		if cards[i].FormCode != cards[j].FormCode {
			return cards[i].FormCode < cards[j].FormCode
		}
		return cards[i].ScopeKey < cards[j].ScopeKey
	})
	routes := map[string][]RouteStep{}
	out := make([]RouteSchemeCard, 0, len(cards))
	type pending struct {
		card   Card
		owners map[string]int64
		err    error
	}
	list := make([]pending, 0, len(cards))
	ids := map[int64]bool{}
	for _, c := range cards {
		if c.FormCode != TemplateMP && c.FormCode != TemplateRetail {
			continue // п. 4.3 — розница и МП
		}
		if !filter.AllowsCard(c) {
			continue
		}
		if _, ok := routes[c.FormCode]; !ok {
			rt, err := h.cards.Route(ctx, c.FormCode)
			if err != nil {
				writeErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			routes[c.FormCode] = rt
		}
		var owners map[string]int64
		var oerr error
		if h.owners != nil {
			owners, oerr = h.owners.StepOwners(ctx, c.PlID, c.FormCode, c.ScopeKey)
		}
		for _, uid := range owners {
			ids[uid] = true
		}
		list = append(list, pending{card: c, owners: owners, err: oerr})
	}
	idList := make([]int64, 0, len(ids))
	for id := range ids {
		idList = append(idList, id)
	}
	names := h.userNames(ctx, idList)
	for _, it := range list {
		sc := buildRouteScheme(it.card, routes[it.card.FormCode], it.owners, names)
		if it.err != nil {
			sc.Error = "исполнители не определены: " + it.err.Error()
		}
		out = append(out, sc)
	}
	writeJSON(w, http.StatusOK, map[string]any{"pl_id": plID, "cards": out})
}

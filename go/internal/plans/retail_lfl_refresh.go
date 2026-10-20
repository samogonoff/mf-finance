package plans

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Обновление LFL-статусов магазинов во ВСЕХ периодах (разовая операция администратора).
//
// Обычная синхронизация приводит строки к справочнику только у карточек, которые ещё можно редактировать,
// поэтому у согласованных и закрытых периодов остались статусы из старого справочника ЦФО («новый» почти у
// всех). Здесь по всем строкам форм меняется ТОЛЬКО tp_row.lfl_status — на актуальный статус из Лисы.
// Значения ячеек, версии и листы согласования карточек, снимки версий и ручные переопределения LFL не
// затрагиваются; меняется то, что считается от статуса (LFL, % выполнения, подсветка), поэтому есть
// предпросмотр: сначала показываем, что изменится, и только потом применяем.

// RetailLFLRefreshRow — строка формы любого периода для сверки статуса.
type RetailLFLRefreshRow struct {
	RowID      int64
	CardID     int64
	CodeCFO    int
	Status     string
	DateOpen   string // дата открытия магазина (пусто — неизвестна)
	Country    string
	Year       int
	Month      int
	CardStatus string
	Editable   bool
}

// retailLFLComparableMonths — с какого возраста магазин сравнимый (LFL): открыт не позже чем за столько месяцев
// до начала периода.
const retailLFLComparableMonths = 12

// retailLFLRefreshAllowed — можно ли перенести статус из Лисы в строку ПРОШЛОГО периода.
//
// Статус Лисы — сегодняшний, а часть статусов зависит от времени: «закрыт» и «реконструкция» относятся к
// определённым датам, «до года» и «новый» — к возрасту магазина на момент периода. Переносить их в прошлое
// нельзя: магазин, закрытый в сентябре, исчез бы из LFL и % выполнения в утверждённых планах до сентября.
// Исправляется только исходный артефакт — устаревшее «новый» (или пустой статус) у магазина, который в
// периоде уже был сравнимым: открыт не менее чем за 12 месяцев до начала периода (дата открытия неизвестна —
// тоже считаем сравнимым: в справочнике ЦФО её нет у большинства действующих магазинов).
func retailLFLRefreshAllowed(r RetailLFLRefreshRow, have, want string) bool {
	if want != LFLYes || (have != LFLNew && have != "") {
		return false
	}
	opened, ok := parseRetailDate(r.DateOpen)
	if !ok {
		return true
	}
	periodStart := time.Date(r.Year, time.Month(r.Month), 1, 0, 0, 0, 0, time.UTC)
	return !opened.After(periodStart.AddDate(0, -retailLFLComparableMonths, 0))
}

// RetailLFLChange — изменение статуса одной строки.
type RetailLFLChange struct {
	RowID int64  `json:"row_id"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// RetailLFLTransition — сколько строк перешло из статуса в статус.
type RetailLFLTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
	Rows int    `json:"rows"`
}

// RetailLFLPeriod — изменения по карточке периода.
type RetailLFLPeriod struct {
	CardID     int64  `json:"card_id"`
	Country    string `json:"country"`
	Year       int    `json:"year"`
	Month      int    `json:"month"`
	CardStatus string `json:"card_status"`
	Editable   bool   `json:"editable"` // false — период уже закрыт для правок (утверждён, на согласовании)
	Changed    int    `json:"changed"`
}

// RetailLFLRefreshResult — итог (предпросмотра или применения).
type RetailLFLRefreshResult struct {
	DryRun       bool                  `json:"dry_run"`
	StoresInLisa int                   `json:"stores_in_lisa"`
	RowsScanned  int                   `json:"rows_scanned"`
	// RowsSkipped — строки, где статус Лисы отличается, но он зависит от времени и в прошлые периоды не переносится
	// («закрыт», «реконструкция», «до года», «новый»; «LFL» — только магазинам, ещё не сравнимым в периоде).
	RowsSkipped  int                   `json:"rows_skipped"`
	RowsChanged  int                   `json:"rows_changed"`
	CardsChanged int                   `json:"cards_changed"`
	LockedCards  int                   `json:"locked_cards"` // из изменённых: уже не редактируемые карточки
	Transitions  []RetailLFLTransition `json:"transitions"`
	Periods      []RetailLFLPeriod     `json:"periods"`
}

// planRetailLFLRefresh — чистый расчёт изменений. lisa — канонический статус магазина из Лисы по коду
// ЦФО (только найденные в Лисе). Пустой статус Лисы и магазины без карточки строку не меняют: стирать
// статус и подставлять нечего.
func planRetailLFLRefresh(rows []RetailLFLRefreshRow, lisa map[int]string) ([]RetailLFLChange, RetailLFLRefreshResult) {
	res := RetailLFLRefreshResult{StoresInLisa: len(lisa), RowsScanned: len(rows), Transitions: []RetailLFLTransition{}, Periods: []RetailLFLPeriod{}}
	var changes []RetailLFLChange
	trans := map[[2]string]int{}
	periods := map[int64]*RetailLFLPeriod{}
	for _, r := range rows {
		want, ok := lisa[r.CodeCFO]
		if !ok || want == "" {
			continue
		}
		have := retailLFLStatus(r.Status)
		if have == want {
			continue
		}
		if !retailLFLRefreshAllowed(r, have, want) {
			res.RowsSkipped++
			continue
		}
		changes = append(changes, RetailLFLChange{RowID: r.RowID, From: r.Status, To: want})
		trans[[2]string{have, want}]++
		p := periods[r.CardID]
		if p == nil {
			p = &RetailLFLPeriod{CardID: r.CardID, Country: r.Country, Year: r.Year, Month: r.Month, CardStatus: r.CardStatus, Editable: r.Editable}
			periods[r.CardID] = p
		}
		p.Changed++
	}
	res.RowsChanged = len(changes)
	for k, n := range trans {
		res.Transitions = append(res.Transitions, RetailLFLTransition{From: k[0], To: k[1], Rows: n})
	}
	sort.Slice(res.Transitions, func(i, j int) bool {
		if res.Transitions[i].Rows != res.Transitions[j].Rows {
			return res.Transitions[i].Rows > res.Transitions[j].Rows
		}
		return res.Transitions[i].From+res.Transitions[i].To < res.Transitions[j].From+res.Transitions[j].To
	})
	for _, p := range periods {
		res.Periods = append(res.Periods, *p)
		res.CardsChanged++
		if !p.Editable {
			res.LockedCards++
		}
	}
	sort.Slice(res.Periods, func(i, j int) bool {
		a, b := res.Periods[i], res.Periods[j]
		if a.Year != b.Year {
			return a.Year > b.Year
		}
		if a.Month != b.Month {
			return a.Month > b.Month
		}
		return a.Country < b.Country
	})
	return changes, res
}

// Ошибки обновления: источник без Лисы (конфигурация) и недоступная Лиса (внешняя система) — разные вещи,
// ошибки собственной БД — третье.
var (
	errRetailRefreshUnsupported = errors.New("обновление статусов доступно только с живым источником (Лиса)")
	errRetailLisaUnavailable    = errors.New("карточки Лисы недоступны, статусы не обновлены")
)

// RefreshLFLStatuses — обновление статусов по всем периодам. apply=false — предпросмотр без записи.
func (s *RetailService) RefreshLFLStatuses(ctx context.Context, p Principal, apply bool) (RetailLFLRefreshResult, error) {
	src, ok := s.source.(RetailLisaStores)
	if !ok {
		return RetailLFLRefreshResult{}, errRetailRefreshUnsupported
	}
	stores, err := src.StoresFromLisa(ctx)
	if err != nil {
		return RetailLFLRefreshResult{}, fmt.Errorf("%w: %v", errRetailLisaUnavailable, err)
	}
	lisa := make(map[int]string, len(stores))
	for _, st := range stores {
		if st.CodeCFO != 0 && st.LisaFound {
			lisa[st.CodeCFO] = retailLFLStatus(st.LFLStatus)
		}
	}
	rows, err := s.repo.LFLRefreshRows(ctx)
	if err != nil {
		return RetailLFLRefreshResult{}, err
	}
	changes, res := planRetailLFLRefresh(rows, lisa)
	res.DryRun = !apply
	if !apply || len(changes) == 0 {
		return res, nil
	}
	if _, err := s.repo.ApplyLFLStatuses(ctx, changes); err != nil {
		return RetailLFLRefreshResult{}, err
	}
	log.Printf("plans retail: LFL-статусы обновлены по всем периодам из Лисы: строк %d, карточек %d (закрытых для правок %d), пользователь %d",
		res.RowsChanged, res.CardsChanged, res.LockedCards, p.UserID)
	s.rec(ctx, p.UserID, "retail_lfl_refresh_all", 0)
	s.InvalidateCache()
	return res, nil
}

// LFLRefresh — POST /api/plans/retail/lfl-refresh?dry_run=1. Только администратор планов.
func (h *RetailHandler) LFLRefresh(w http.ResponseWriter, r *http.Request) {
	dry := r.URL.Query().Get("dry_run")
	apply := !(dry == "1" || strings.EqualFold(dry, "true"))
	res, err := h.svc.RefreshLFLStatuses(r.Context(), h.principal(r), apply)
	if err != nil {
		switch {
		case errors.Is(err, errRetailRefreshUnsupported):
			writeErr(w, http.StatusConflict, err.Error())
		case errors.Is(err, errRetailLisaUnavailable):
			writeErr(w, http.StatusBadGateway, err.Error())
		default:
			writeErr(w, http.StatusInternalServerError, "обновление статусов не выполнено: "+err.Error())
		}
		return
	}
	if apply {
		h.audit(r, "retail_lfl_refresh_all", 0)
	}
	writeJSON(w, http.StatusOK, res)
}

package plans

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
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
	Country    string
	Year       int
	Month      int
	CardStatus string
	Editable   bool
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

// RefreshLFLStatuses — обновление статусов по всем периодам. apply=false — предпросмотр без записи.
func (s *RetailService) RefreshLFLStatuses(ctx context.Context, p Principal, apply bool) (RetailLFLRefreshResult, error) {
	src, ok := s.source.(RetailLisaStores)
	if !ok {
		return RetailLFLRefreshResult{}, errors.New("обновление статусов доступно только с живым источником (Лиса)")
	}
	stores, err := src.StoresFromLisa(ctx)
	if err != nil {
		return RetailLFLRefreshResult{}, fmt.Errorf("карточки Лисы недоступны, статусы не обновлены: %w", err)
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
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if apply {
		h.audit(r, "retail_lfl_refresh_all", 0)
	}
	writeJSON(w, http.StatusOK, res)
}

package plans

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Справочные экраны итогового ТЗ доработок кабинета:
//   - п. 5.2 — список LFL-статусов ИЗ СПРАВОЧНИКА FinDWH.dbo.[001 CodeCFO]
//     (а не из файла), чтобы закрыть вопрос «откуда взялся статус»;
//   - п. 5.1 — раздел «Стратегические планы»: стратегия по площадкам МП
//     (доли статей прямых затрат «НовСтрат» по месяцам — тот же источник, что
//     даёт доли по умолчанию в форме МП, п. 2.1).

// RetailLFLStatusCount — статус LFL справочника и сколько магазинов с ним.
type RetailLFLStatusCount struct {
	Raw       string         `json:"raw"`       // как в справочнике
	Canonical string         `json:"canonical"` // как понимает форма
	Stores    int            `json:"stores"`
	ByCountry map[string]int `json:"by_country"`
}

// summarizeLFLStatuses — группировка магазинов справочника по LfLStatus.
func summarizeLFLStatuses(stores []RetailStore) []RetailLFLStatusCount {
	idx := map[string]*RetailLFLStatusCount{}
	for _, st := range stores {
		if isRetailTotalRow(st) {
			continue
		}
		raw := st.LFLRaw
		if raw == "" {
			raw = st.LFLStatus
		}
		if raw == "" {
			raw = "(пусто)"
		}
		c, ok := idx[raw]
		if !ok {
			c = &RetailLFLStatusCount{Raw: raw, Canonical: retailLFLStatus(st.LFLRaw), ByCountry: map[string]int{}}
			if st.LFLRaw == "" {
				c.Canonical = st.LFLStatus
			}
			idx[raw] = c
		}
		c.Stores++
		c.ByCountry[st.Country]++
	}
	out := make([]RetailLFLStatusCount, 0, len(idx))
	for _, c := range idx {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Stores != out[j].Stores {
			return out[i].Stores > out[j].Stores
		}
		return out[i].Raw < out[j].Raw
	})
	return out
}

// LFLStatuses — статусы LFL по справочнику магазинов (все страны).
func (s *RetailService) LFLStatuses(ctx context.Context) ([]RetailLFLStatusCount, error) {
	if s.source == nil {
		return nil, nil
	}
	stores, err := s.source.Stores(ctx, "")
	if err != nil {
		return nil, err
	}
	return summarizeLFLStatuses(stores), nil
}

// LFLStatusesGet — GET /api/plans/retail/lfl-statuses.
func (h *RetailHandler) LFLStatusesGet(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.LFLStatuses(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, "справочник магазинов недоступен: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source": "FinDWH.dbo.[001 CodeCFO].LfLStatus", "statuses": list})
}

// StrategyMpMonth — доли статей стратегии по площадкам на месяц.
type StrategyMpMonth struct {
	Month  int                        `json:"month"`
	Shares map[int]map[string]float64 `json:"shares"` // code_cfo → block_type → доля
}

// StrategyHandler — раздел «Стратегические планы» (п. 5.1).
type StrategyHandler struct {
	shares *MpShareDefaults
}

// NewStrategyHandler — конструктор.
func NewStrategyHandler(shares *MpShareDefaults) *StrategyHandler {
	return &StrategyHandler{shares: shares}
}

// MpShares — GET /api/plans/strategy/mp-shares?year=2026: доли статей прямых
// затрат площадок по месяцам года из стратегического сценария.
func (h *StrategyHandler) MpShares(w http.ResponseWriter, r *http.Request) {
	year, _ := strconv.Atoi(r.URL.Query().Get("year"))
	if year < 2000 || year > 2100 {
		year = time.Now().Year()
	}
	months := make([]StrategyMpMonth, 0, 12)
	for m := 1; m <= 12; m++ {
		months = append(months, StrategyMpMonth{Month: m, Shares: h.shares.For(r.Context(), year, m)})
	}
	platforms := MarketplaceSeed()
	writeJSON(w, http.StatusOK, map[string]any{
		"year": year, "source": h.shares.SourceLabel(), "platforms": platforms, "months": months,
	})
}

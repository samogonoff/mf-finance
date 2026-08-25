package plans

import (
	"context"
	"errors"
	"strconv"
)

// Провайдер синхронизации справочника магазинов розницы (ТЗ §11:
// «синхронизация раз в сутки + кнопка, версионирование строк»).
//
// Своей логики версионирования тут нет: она уже есть в общем движке sync.go
// (полная пересборка среза в транзакции, diff added/changed/removed по
// external_id, журнал plans_dir_sync_log, backoff). Провайдер отвечает только
// за «откуда брать строки» — источник ТЗ §11 через RetailDataSource, то есть в
// mock-режиме синхронизация тоже работает.

// RetailStoreProvider — провайдер dir_retail_store.
type RetailStoreProvider struct {
	src RetailDataSource
}

// NewRetailStoreProvider — конструктор.
func NewRetailStoreProvider(src RetailDataSource) *RetailStoreProvider {
	return &RetailStoreProvider{src: src}
}

// Code — код справочника.
func (p *RetailStoreProvider) Code() string { return "dir_retail_store" }

// Fetch — магазины ВСЕХ стран розницы: справочник один, а форм четыре (ТЗ §1),
// поэтому срез не сужаем по стране — иначе синхронизация одной страны вычищала
// бы строки остальных.
func (p *RetailStoreProvider) Fetch(ctx context.Context) ([]SyncRow, error) {
	if p.src == nil {
		return nil, errors.New("источник справочника магазинов не настроен")
	}
	// Пустая страна = все страны (реализации отдают всё, что есть).
	stores, err := p.src.Stores(ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]SyncRow, 0, len(stores))
	for _, st := range stores {
		if st.CodeCFO == 0 {
			continue // без CodeCFO строка бесполезна: это ключ строки формы (V-04)
		}
		out = append(out, SyncRow{
			ExternalID: strconv.Itoa(st.CodeCFO),
			Payload: map[string]any{
				"code_cfo":    st.CodeCFO,
				"klient_id":   st.KlientID,
				"cfo":         st.NameCFO,
				"group_cfo1":  st.GroupCFO1,
				"city":        st.City,
				"country":     st.Country,
				"code_fox":    st.CodeFOX,
				"ploschad":    st.Ploschad,
				"store_type":  st.StoreType,
				"date_open":   st.DateOpen,
				"date_close":  st.DateClose,
				"stage":       st.Stage,
				"company_mf":  st.CompanyMF,
				"channel":     st.Channel,
				"cfo_old":     st.CFOold,
				"category":    st.Category,
				"lfl_status":  st.LFLStatus,
				"reg_manager": st.RegManager,
				"manager":     st.Manager,
				"pl_analytic": st.PLAnalytic,
			},
		})
	}
	return out, nil
}

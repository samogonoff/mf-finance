package plans

import (
	"context"
	"errors"
	"log"
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
	// dir — справочники кабинета: из них берётся ПРАВИЛО состава розницы
	// (dir_retail_scope). nil → работаем по правилу ТЗ из RetailScopeSeed.
	dir dirRowsSource
}

// NewRetailStoreProvider — конструктор.
func NewRetailStoreProvider(src RetailDataSource) *RetailStoreProvider {
	return &RetailStoreProvider{src: src}
}

// WithScopeSource подключает справочник состава розницы (правка ТЗ §11).
func (p *RetailStoreProvider) WithScopeSource(dir dirRowsSource) *RetailStoreProvider {
	p.dir = dir
	return p
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
	// Состав розницы — правило ALL_view (правка ТЗ §11), а не одна группа ЦФО:
	// группа магазинов + восемь ЦФО-отделов − ЦФО 40 и 722. Без него в справочник
	// попадал другой состав, и суммы формы не сходились с отчётами.
	scope := NewRetailScope(scopeRulesFromDir(ctx, p.dir))

	// Страна закрытых магазинов берётся по коду ЦФО (правка 08.09.2026 п. 35):
	// в колонке страны у них стоит название группы.
	cfoCountry := retailCfoCountries(ctx, p.dir)

	out := make([]SyncRow, 0, len(stores))
	outOfScope, totals := 0, 0
	for _, st := range stores {
		if st.CodeCFO == 0 {
			continue // без CodeCFO строка бесполезна: это ключ строки формы (V-04)
		}
		// Строки-итоги по стране и по статусу (правка 08.09.2026 п. 34): их
		// CodeCFO заполнен, и без этой проверки каждый магазин попадал в сумму
		// дважды — май 2026 по РБ давал 68 467 424 вместо 34 233 712.
		if isRetailTotalRow(st) {
			totals++
			continue
		}
		if !scope.Includes(st.CodeCFO, st.GroupCFO1) {
			outOfScope++
			continue
		}
		st.Country = retailStoreCountry(st, cfoCountry)
		st.LFLStatus = retailStoreLFL(st)
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
				// Отбор строк формы идёт по СТАТУСУ справочника, а не по наличию
				// факта (правка ТЗ §12): иначе новые магазины в план не попадут.
				"closed": retailStoreClosed(st),
			},
		})
	}
	if outOfScope > 0 {
		log.Printf("plans retail: вне состава розницы (правило dir_retail_scope) пропущено ЦФО: %d", outOfScope)
	}
	// Журнал импорта (правка п. 34): сколько строк принято и сколько отсеяно —
	// иначе расхождение с ожидаемым составом (РБ 192, РФ 74, закрыты 55, KZ 45,
	// UZ 6) всплывает только в контрольной сверке полноты.
	log.Printf("plans retail: справочник магазинов — принято %d, строк-итогов отсеяно %d, вне состава %d",
		len(out), totals, outOfScope)
	return out, nil
}

// retailCfoCountries — страна по коду ЦФО из справочника кабинета (dir_cfo).
// Справочник недоступен — карта пустая, и страна остаётся той, что дал источник.
func retailCfoCountries(ctx context.Context, dir dirRowsSource) map[int]string {
	out := map[int]string{}
	if dir == nil {
		return out
	}
	rows, err := dir.Rows(ctx, "dir_cfo")
	if err != nil {
		log.Printf("plans retail: справочник ЦФО недоступен, страна закрытых магазинов не уточняется: %v", err)
		return out
	}
	for _, r := range rows {
		if code := intAt(r.Payload, "code_cfo"); code > 0 {
			if c := strAt(r.Payload, "country"); c != "" {
				out[code] = c
			}
		}
	}
	return out
}

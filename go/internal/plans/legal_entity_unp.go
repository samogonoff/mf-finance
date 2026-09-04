package plans

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// УНП ЮЛ страны для колонки «Компания» приёмника тактики (правки ТЗ 02.09.2026
// Розница §15/§16, МП §19).
//
// Было: план пишется на фиктивное ЮЛ либо ЮЛ не пишется вовсе. Верно: реальный
// УНП — BY 690719790 «ФОРМЭЛЬ ООО» (F), KZ 141240004842 (MFKaz), RU 6950135110
// (TDMF), UZ 305554644 (MFUz). Фиктивные коды (1 BY, 2 RU, 4 CN, 7 KZ, 8 UZ,
// 9 TR) годятся ТОЛЬКО для чтения исторических строк, поэтому строки справочника
// с historical_only здесь отбрасываются: на запись их брать нельзя.
//
// Источник — справочник dir_legal_entity (миграция 0042), а не карта в коде:
// смена ЮЛ страны — это данные. Сид остаётся фолбэком на недоступный справочник.

// LegalEntityUNPSeed — реальные ЮЛ контура (правка §15).
func LegalEntityUNPSeed() map[string]string {
	return map[string]string{
		"BY": "690719790",    // ФОРМЭЛЬ ООО (F) — розница РБ и маркетплейсы (ЦФО 250)
		"KZ": "141240004842", // MFKaz
		"RU": "6950135110",   // TDMF
		"UZ": "305554644",    // MFUz
	}
}

// dirLegalEntities — резолвер УНП из справочника с кэшем.
type dirLegalEntities struct {
	repo dirRowsSource

	mu        sync.RWMutex
	loadedAt  time.Time
	byCountry map[string]string
	ttl       time.Duration
}

// NewLegalEntityResolver — резолвер УНП. repo=nil → работа на сиде.
func NewLegalEntityResolver(repo dirRowsSource) LegalEntityResolver {
	return &dirLegalEntities{repo: repo, ttl: time.Minute}
}

// UNP — УНП ЮЛ страны. Второй результат false, если реального ЮЛ нет: тогда
// публикация обязана остановиться, а не записать план на пустую «Компанию».
func (r *dirLegalEntities) UNP(ctx context.Context, country string) (string, bool) {
	key := strings.ToUpper(strings.TrimSpace(country))
	if key == "" {
		return "", false
	}
	m := r.load(ctx)
	unp, ok := m[key]
	return unp, ok && strings.TrimSpace(unp) != ""
}

func (r *dirLegalEntities) load(ctx context.Context) map[string]string {
	r.mu.RLock()
	fresh := time.Since(r.loadedAt) < r.ttl && r.byCountry != nil
	m := r.byCountry
	r.mu.RUnlock()
	if fresh {
		return m
	}

	out := LegalEntityUNPSeed()
	if r.repo != nil {
		rows, err := r.repo.Rows(ctx, "dir_legal_entity")
		if err != nil {
			log.Printf("plans: справочник ЮЛ недоступен, УНП берём из сида: %v", err)
		} else {
			fromDir := map[string]string{}
			for _, row := range rows {
				// Фиктивные коды — только чтение истории, на запись не годятся.
				if boolAt(row.Payload, "historical_only") {
					continue
				}
				unp := strings.TrimSpace(strAt(row.Payload, "unp"))
				country := strings.ToUpper(strings.TrimSpace(strAt(row.Payload, "country")))
				if unp == "" || country == "" {
					continue
				}
				// Две реальные записи на страну — конфликт данных: план уедет не на
				// то ЮЛ. Оставляем первую и говорим об этом, а не выбираем молча.
				if prev, ok := fromDir[country]; ok && prev != unp {
					log.Printf("plans: в справочнике ЮЛ два реальных УНП для %s (%s и %s) — используется %s", country, prev, unp, prev)
					continue
				}
				fromDir[country] = unp
			}
			if len(fromDir) > 0 {
				out = fromDir
			}
		}
	}

	r.mu.Lock()
	r.byCountry, r.loadedAt = out, time.Now()
	r.mu.Unlock()
	return out
}

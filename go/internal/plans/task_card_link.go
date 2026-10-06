package plans

import (
	"context"
	"fmt"
	"strconv"
)

// Связка «задание → форма»: какую карточку и какой экран открывать по кнопке
// «Форма» в списке заданий.
//
// Зачем отдельно. У МП задание одно на весь блок, и форма адресуется самим
// заданием (`/plans/mp-form/{taskID}`). У розницы форм ЧЕТЫРЕ — по одной на
// страну (ТЗ §2.0), и адресуются они карточкой, а не заданием. Пока связки не
// было, у розничного задания в интерфейсе отсутствовала кнопка перехода —
// вносить данные было физически негде, хотя и форма, и API уже работали.

// attachFormLinks проставляет заданиям карточку и адрес формы.
// Ошибки поиска карточки не критичны: кнопка просто не появится, а список
// заданий отрисуется — это лучше, чем 500 на весь экран.
func (s *TaskStore) attachFormLinks(ctx context.Context, tasks []Task) {
	if len(tasks) == 0 || s.cards == nil {
		return
	}
	for i := range tasks {
		t := &tasks[i]
		switch t.FormCode {
		case TemplateMP:
			// Форма МП открывается по заданию; карточку всё равно приложим —
			// по ней экран показывает статус, условия и общие затраты.
			segments := s.segmentsByCfo(ctx, t.CfoCodes)
			for _, scope := range mpTaskScopes(t.CfoCodes, segments) {
				if c, err := s.cards.CardByScope(ctx, t.PlID, TemplateMP, scope); err == nil {
					t.CoveredCardIDs = append(t.CoveredCardIDs, c.ID)
					if t.CardID == 0 {
						t.CardID = c.ID
					}
				}
			}
			t.FormPath = fmt.Sprintf("/plans/mp-form/%d", t.ID)
		case TemplateExp:
			// Тот же код формы носят ДВА шаблона задания (миграция 0027):
			// «Затраты ЦП» на этапе 1.5 и «Затраты ЦЗ» на этапе 2.3. Форма
			// построена по ТЗ этапа 1.5 — про центры ПРИБЫЛИ, — и вести на неё
			// задание по центрам затрат нельзя: человек открыл бы чужой комплект
			// и решил, что его строки потерялись.
			if t.StageCode != expPeriodStage {
				continue
			}
			// Задание на этапе одно, а форм четыре (розница РБ и три страны
			// маркетплейсов) — по 145 ЦФО задания страну не выбрать, поэтому
			// ведём на экран комплекта, откуда открывается нужная карточка.
			t.FormPath = fmt.Sprintf("/plans/exp/%d", t.PlID)
		case TemplateRetail:
			country := s.taskCountry(ctx, *t)
			if country == "" {
				continue // задание на все страны (старый шаблон) — форму не выбрать
			}
			c, err := s.cards.CardByScope(ctx, t.PlID, TemplateRetail, country)
			if err != nil {
				continue
			}
			t.CardID = c.ID
			t.FormPath = fmt.Sprintf("/plans/retail-form/%d", c.ID)
		}
	}
}

func mpTaskScopes(codes []int, segments map[int]string) []string {
	seen := map[string]bool{}
	for _, code := range codes {
		scope := segments[code]
		if scope == "" {
			for _, platform := range MarketplaceSeed() {
				if platform.CodeCFO == code {
					scope = platform.Segment
					break
				}
			}
		}
		if scope == "large" || scope == "small" {
			seen[scope] = true
		}
	}
	out := make([]string, 0, 2)
	for _, scope := range []string{"large", "small"} {
		if seen[scope] {
			out = append(out, scope)
		}
	}
	return out
}

// taskCountry — страна задания по его ЦФО. Берём страну первого магазина из
// справочника: задание розницы формируется фильтром по стране, поэтому все его
// ЦФО принадлежат одной стране. Если код нечисловой или ЦФО нет — пусто.
func (s *TaskStore) taskCountry(ctx context.Context, t Task) string {
	if len(t.CfoCodes) == 0 {
		return ""
	}
	// Код передаём СТРОКОЙ: external_id — text, а pgx не приводит int к
	// текстовому параметру и просто возвращает ошибку типов (запрос при этом
	// выглядит рабочим, если проверять его в psql руками).
	var country string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(r.payload_json->>'country','')
		  FROM plans_directory_row r
		  JOIN plans_directory d ON d.id = r.directory_id
		 WHERE d.code = 'dir_cfo' AND r.external_id = $1
		 LIMIT 1`, strconv.Itoa(t.CfoCodes[0])).Scan(&country)
	if err != nil {
		return ""
	}
	return country
}

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
			if c, err := s.mpCardFor(ctx, t.PlID); err == nil {
				t.CardID = c
			}
			t.FormPath = fmt.Sprintf("/plans/mp-form/%d", t.ID)
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

// mpCardFor — карточка МП периода: сначала large, иначе small.
func (s *TaskStore) mpCardFor(ctx context.Context, plID int64) (int64, error) {
	for _, scope := range []string{"large", "small"} {
		if c, err := s.cards.CardByScope(ctx, plID, TemplateMP, scope); err == nil {
			return c.ID, nil
		}
	}
	return 0, fmt.Errorf("карточка МП периода %d не найдена", plID)
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

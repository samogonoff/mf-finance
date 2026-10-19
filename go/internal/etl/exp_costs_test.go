package etl

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestExpCostsValue(t *testing.T) {
	d := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		in   any
		kind colKind
		want sql.NullString
	}{
		{nil, kStr, sql.NullString{}},
		{d, kDate, sql.NullString{String: "2025-03-01", Valid: true}},
		{[]byte(" 12.50 "), kDec, sql.NullString{String: "12.50", Valid: true}},
		{float64(1234567.891), kDec, sql.NullString{String: "1234567.891", Valid: true}},
		{float64(1e9), kDec, sql.NullString{String: "1000000000", Valid: true}}, // без экспоненты
		{int64(42), kInt, sql.NullString{String: "42", Valid: true}},
		{true, kStr, sql.NullString{String: "1", Valid: true}},
		{"  Магазины ", kStr, sql.NullString{String: "Магазины", Valid: true}},
		// Нечисловой мусор в числовой колонке даёт 0, а не отказ всего батча в CH.
		{"n/a", kInt, sql.NullString{String: "0", Valid: true}},
		{"", kInt, sql.NullString{String: "0", Valid: true}},
		{" 101.0 ", kInt, sql.NullString{String: "101", Valid: true}},
		{float64(250), kInt, sql.NullString{String: "250", Valid: true}},
		{"12,5", kDec, sql.NullString{String: "12.5", Valid: true}},
		{"abc", kDec, sql.NullString{String: "0", Valid: true}},
		{nil, kInt, sql.NullString{}},
	}
	for _, c := range cases {
		if got := expCostsValue(c.in, c.kind); got != c.want {
			t.Errorf("%v: got %+v, want %+v", c.in, got, c.want)
		}
	}
}

// Колонки процедуры ищутся без учёта регистра; нет обязательной — ошибка с перечнем;
// необязательных (например, company, которых процедура не отдаёт) может не быть.
func TestExpCostsLayout(t *testing.T) {
	cols := []string{"HalfYear", "scenario", "COUNTRY", "Month", "CodePL", "GroupPL", "Expense", "CodeCFO", "GroupCFO1", "Amount"}
	idx, err := expCostsLayout(cols)
	if err != nil {
		t.Fatal(err)
	}
	if idx[0] != 1 || idx[1] != 2 || idx[2] != 3 {
		t.Fatalf("позиции: %v", idx)
	}
	for i, c := range expCostsCols {
		if c.Name == "cfo" && idx[i] != -1 {
			t.Errorf("отсутствующая необязательная колонка cfo должна быть -1, got %d", idx[i])
		}
	}
	if _, err := expCostsLayout([]string{"Scenario", "Month"}); err == nil {
		t.Fatal("ждали ошибку про обязательные колонки")
	}
}

func TestExpCostsOptsDefaults(t *testing.T) {
	var o ExpCostsOpts
	o.normalize()
	if o.AfterHour != 5 || o.BeforeHour != 9 || o.MinYear != 2024 || o.Proc == "" {
		t.Fatalf("дефолты: %+v", o)
	}
}

func TestExpCostsSwapAllowed(t *testing.T) {
	cases := []struct {
		rows, have int64
		ok         bool
	}{
		{771165, 0, true},      // первая заливка
		{771165, 760000, true}, // обычный день
		{400000, 771165, true}, // сократилось, но меньше чем вдвое не упало
		{300000, 771165, false},
		{0, 771165, false}, // пустой ответ процедуры
		{0, 0, false},
	}
	for _, c := range cases {
		if err := expCostsSwapAllowed(c.rows, c.have); (err == nil) != c.ok {
			t.Errorf("rows=%d have=%d: err=%v, want ok=%v", c.rows, c.have, err, c.ok)
		}
	}
}

// Колонка проекта: и ProjectName (действующая выдача), и Projects (как в ТЗ) попадают в
// project_name — иначе фильтр проектов молча пропустил бы всё и суммы задвоились бы.
func TestExpCostsLayout_ProjectColumnAlternatives(t *testing.T) {
	base := []string{"Scenario", "Country", "Month", "CodePL", "CodeCFO", "GroupCFO1", "Amount"}
	for _, name := range []string{"ProjectName", "Projects"} {
		idx, err := expCostsLayout(append(append([]string{}, base...), name))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for i, c := range expCostsCols {
			if c.Name == "project_name" && idx[i] == len(base) {
				found = true
			}
		}
		if !found {
			t.Errorf("колонка %s не сопоставлена project_name", name)
		}
	}
}

// Кнопка «Обновить из источника», пока идёт чужая заливка, ждёт её окончания (и свежий снимок
// получает), а не отвечает ошибкой «заливка уже идёт»; ожидание прерывается по контексту.
func TestExpCostsRefreshNow_WaitsForRunningLoad(t *testing.T) {
	w := NewExpCostsWorker(Deps{CHURL: "http://localhost:1"}, ExpCostsOpts{})
	expCostsRunSem <- struct{}{} // чужая заливка идёт
	done := make(chan error, 1)
	go func() { done <- w.RefreshNow(context.Background()) }()
	select {
	case err := <-done:
		t.Fatalf("RefreshNow не дождался окончания чужой заливки: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	<-expCostsRunSem // чужая заливка закончилась
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ждали nil, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RefreshNow не вернулся после окончания чужой заливки")
	}

	expCostsRunSem <- struct{}{}
	defer func() { <-expCostsRunSem }()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if err := w.RefreshNow(ctx); err == nil {
		t.Fatal("ожидание должно прерываться по контексту")
	}
}
package debt

import (
	"testing"
	"time"
)

func TestParseObjectsName_StandardOutgoing(t *testing.T) {
	// Реальный sample из [FinDWH].[dbo].[Objects].Name для исходящих 1С-документов.
	p, ok := ParseObjectsName("Реализация (акт, накладная, УПД) ТДБП-003950 от 15.04.2026 10:00:00")
	if !ok {
		t.Fatal("должен распарсить стандартный outbound")
	}
	if p.Kind != "Реализация (акт, накладная, УПД)" {
		t.Errorf("Kind = %q", p.Kind)
	}
	if p.Number != "ТДБП-003950" {
		t.Errorf("Number = %q", p.Number)
	}
	want := time.Date(2026, 4, 15, 10, 0, 0, 0, time.UTC)
	if !p.Date.Equal(want) {
		t.Errorf("Date = %v, want %v", p.Date, want)
	}
	if p.Source != "objects" {
		t.Errorf("Source = %q", p.Source)
	}
}

func TestParseObjectsName_DateWithoutTime(t *testing.T) {
	p, ok := ParseObjectsName("Поступление товаров ПТ-005678 от 10.04.2026")
	if !ok {
		t.Fatal("должен распарсить без времени")
	}
	if p.Number != "ПТ-005678" {
		t.Errorf("Number = %q", p.Number)
	}
	want := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	if !p.Date.Equal(want) {
		t.Errorf("Date = %v, want %v", p.Date, want)
	}
}

func TestParseObjectsName_EmptyOrJunk(t *testing.T) {
	if _, ok := ParseObjectsName(""); ok {
		t.Error("пустая строка не должна парситься")
	}
	if _, ok := ParseObjectsName("просто что-то без структуры"); ok {
		t.Error("свободный текст не должен парситься")
	}
}

func TestParseMapping_WithDocSuffix(t *testing.T) {
	in := "Реализация (акт, накладная, УПД) ТДБП-003950 от 15.04.2026 10:00:00 (документ), Транспортные и экспедиционные услуги (субконто)"
	p, ok := ParseMapping(in)
	if !ok {
		t.Fatal("Mapping с (документ) должен парситься")
	}
	if p.Kind != "Реализация (акт, накладная, УПД)" {
		t.Errorf("Kind = %q", p.Kind)
	}
	if p.Number != "ТДБП-003950" {
		t.Errorf("Number = %q", p.Number)
	}
	if p.Source != "mapping" {
		t.Errorf("Source = %q, want mapping", p.Source)
	}
}

func TestParseMapping_WithoutDocSuffix(t *testing.T) {
	// Когда в Mapping нет "(документ)" — только аналитика (например для проводки без документа,
	// типа курсовых разниц или ручных корректировок). Не парсим.
	in := "Основное подразделение (проводка), Курсовые разницы (субконто)"
	if _, ok := ParseMapping(in); ok {
		t.Error("Mapping без (документ) не должен парситься")
	}
}

func TestParseTransDescription_IncomingDoc(t *testing.T) {
	in := "Услуга по организации доставки (экспедированию) груза по вх.д. 0000338/5028 от 01.04.2026"
	p, ok := ParseTransDescription(in)
	if !ok {
		t.Fatal("должен распарсить «по вх.д.»")
	}
	if p.Number != "0000338/5028" {
		t.Errorf("Number = %q", p.Number)
	}
	if p.Kind != "Входящий документ" {
		t.Errorf("Kind = %q, want 'Входящий документ'", p.Kind)
	}
	want := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if !p.Date.Equal(want) {
		t.Errorf("Date = %v, want %v", p.Date, want)
	}
	if p.Source != "trans_description" {
		t.Errorf("Source = %q", p.Source)
	}
}

func TestParseTransDescription_NoIncomingMarker(t *testing.T) {
	if _, ok := ParseTransDescription("Реализация товаров"); ok {
		t.Error("без 'по вх.д.' не должен парситься")
	}
}

func TestResolveDoc_PriorityObjectsFirst(t *testing.T) {
	// Если Objects.Name даёт результат — Mapping/TransDesc игнорируются,
	// даже если они тоже парсились бы.
	objectsName := "Реализация ТДБП-001 от 01.01.2026"
	mapping := "Реализация ТДБП-002 от 02.02.2026 (документ), что-то (субконто)"
	trans := "Услуга по вх.д. 0000003 от 03.03.2026"
	p := ResolveDoc(objectsName, mapping, trans)
	if p.Source != "objects" {
		t.Errorf("Source должен быть objects, got %q", p.Source)
	}
	if p.Number != "ТДБП-001" {
		t.Errorf("Number должен быть из Objects, got %q", p.Number)
	}
}

func TestResolveDoc_FallbackToMapping(t *testing.T) {
	p := ResolveDoc("", "Поступление ПТ-008260 от 10.04.2026 8:35:34 (документ), Транспортные (субконто)", "")
	if p.Source != "mapping" {
		t.Errorf("Source = %q, want mapping", p.Source)
	}
	if p.Number != "ПТ-008260" {
		t.Errorf("Number = %q", p.Number)
	}
}

func TestResolveDoc_FallbackToTransDescription(t *testing.T) {
	p := ResolveDoc("", "Аналитика без документа", "Услуга по вх.д. 12345 от 01.04.2026")
	if p.Source != "trans_description" {
		t.Errorf("Source = %q, want trans_description", p.Source)
	}
	if p.Number != "12345" {
		t.Errorf("Number = %q", p.Number)
	}
}

func TestResolveDoc_FallbackEmpty(t *testing.T) {
	p := ResolveDoc("", "", "")
	if p.Source != "fallback" {
		t.Errorf("Source = %q, want fallback", p.Source)
	}
	if p.Number != "" || p.Kind != "" || !p.Date.IsZero() {
		t.Errorf("fallback должен быть пустой: %+v", p)
	}
}

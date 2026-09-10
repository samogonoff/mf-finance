package plans

import (
	"context"
	"errors"
	"fmt"
)

// Сценарии реестров формы 1.5 (ТЗ §5.5).
//
// Реестр закрывается на ввод одновременно с формой: при отправке на
// согласование, при утверждении и при публикации. Поэтому право на правку
// спрашивается у той же карточки, что и у сетки, — иначе согласующий смог бы
// править реестр утверждённой формы, и её суммы разошлись бы с версией.

// ExpRegistry — реестр целиком: строки плюс посчитанные итоги.
type ExpRegistry struct {
	Kind     string      `json:"kind"`
	Rows     []ExpRegRow `json:"rows"`
	ReadOnly bool        `json:"read_only"`
	// Totals — итоги вида реестра. У ФОТ это суммы по месяцам и за год, у ИТ —
	// перенос/тотал/общие/исключённые; у остальных — итог по месяцам.
	Totals any `json:"totals"`
}

// WithRegistries подключает реестры к сервису формы.
func (s *ExpService) WithRegistries(r ExpRegRepo) *ExpService { s.reg = r; return s }

// Registry — реестр формы по виду.
func (s *ExpService) Registry(ctx context.Context, p Principal, cardID int64, kind string) (ExpRegistry, error) {
	if !ExpRegKindKnown(kind) {
		return ExpRegistry{}, fmt.Errorf("неизвестный реестр %q", kind)
	}
	c, in, err := s.access(ctx, cardID)
	if err != nil {
		return ExpRegistry{}, err
	}
	if s.reg == nil {
		return ExpRegistry{}, errors.New("реестры не подключены")
	}
	rows, err := s.reg.RegRows(ctx, in.ID, kind)
	if err != nil {
		return ExpRegistry{}, err
	}
	return ExpRegistry{
		Kind: kind, Rows: rows, ReadOnly: !cardEditable(c),
		Totals: expRegTotals(kind, rows),
	}, nil
}

// expRegTotals — итоги по виду реестра.
func expRegTotals(kind string, rows []ExpRegRow) any {
	switch kind {
	case ExpRegPayroll:
		return ExpPayrollSum(rows)
	case ExpRegIT:
		return ExpITSum(rows)
	default:
		// Командировки и логистика: итог по месяцам и за год — тем же
		// суммированием, что и ФОТ, только без отбора по виду.
		out := ExpPayrollTotals{ByMonth: map[int]float64{}}
		for _, r := range rows {
			if r.Excluded {
				continue
			}
			for m := 1; m <= 12; m++ {
				v := r.MonthAmount(m)
				out.ByMonth[m] += v
				out.Year += v
			}
		}
		return out
	}
}

// regEditable — общая проверка «реестр открыт на ввод».
func (s *ExpService) regEditable(ctx context.Context, cardID int64) (ExpInstance, error) {
	c, in, err := s.access(ctx, cardID)
	if err != nil {
		return ExpInstance{}, err
	}
	if s.reg == nil {
		return ExpInstance{}, errors.New("реестры не подключены")
	}
	if !cardEditable(c) {
		return ExpInstance{}, errors.New("реестр закрыт на ввод вместе с формой: правка только после возврата или reopen")
	}
	return in, nil
}

// SaveRegRow — создание или правка строки реестра.
func (s *ExpService) SaveRegRow(ctx context.Context, p Principal, cardID int64, r ExpRegRow) (int64, error) {
	in, err := s.regEditable(ctx, cardID)
	if err != nil {
		return 0, err
	}
	if err := validateExpRegRow(r); err != nil {
		return 0, err
	}
	return s.reg.SaveRegRow(ctx, in.ID, r, p.UserID)
}

// DeleteRegRow — удаление строки (с записью в журнал вместе со значениями).
func (s *ExpService) DeleteRegRow(ctx context.Context, p Principal, cardID, rowID int64) error {
	in, err := s.regEditable(ctx, cardID)
	if err != nil {
		return err
	}
	return s.reg.DeleteRegRow(ctx, in.ID, rowID, p.UserID)
}

// CopyRegRow — копия строки со всеми месяцами.
func (s *ExpService) CopyRegRow(ctx context.Context, p Principal, cardID, rowID int64) (int64, error) {
	in, err := s.regEditable(ctx, cardID)
	if err != nil {
		return 0, err
	}
	return s.reg.CopyRegRow(ctx, in.ID, rowID, p.UserID)
}

// validateExpRegRow — проверки, без которых строка бессмысленна.
//
// E-19 (ТЗ §9): ФИО в реестре ФОТ выбирается из справочника, а не вводится
// текстом. Проверяем наличие идентификатора сотрудника, а не непустую строку:
// текст в поле ФИО — ровно то, что валидация запрещает.
//
// E-20 (ТЗ §9): у строки ИТ-реестра не могут стоять оба признака сразу —
// «перенести на 250» и «не учитывать» противоречат друг другу.
func validateExpRegRow(r ExpRegRow) error {
	if !ExpRegKindKnown(r.Kind) {
		return fmt.Errorf("неизвестный реестр %q", r.Kind)
	}
	for m := range r.Months {
		if m < 1 || m > 12 {
			return fmt.Errorf("месяц вне диапазона: %d", m)
		}
	}
	switch r.Kind {
	case ExpRegPayroll:
		if expAttrInt(r.Attrs, "employee_id") <= 0 {
			return errors.New("E-19: ФИО выбирается из справочника сотрудников, а не вводится текстом")
		}
		if le, _ := r.Attrs["legal_entity"].(string); le != "" {
			if _, ok := ExpPayrollPair(le); !ok {
				return fmt.Errorf("юридическое лицо %q не сопоставлено паре статей — строка не будет опубликована", le)
			}
		}
	case ExpRegIT:
		if r.MoveTo250 && r.Excluded {
			return errors.New("E-20: у строки ИТ не могут стоять оба признака — «перенести на ЦФО 250» и «не учитывать»")
		}
	}
	return nil
}

// expAttrInt — целочисленный атрибут строки. JSON отдаёт числа как float64,
// поэтому разбираем оба варианта.
func expAttrInt(attrs map[string]any, key string) int {
	switch v := attrs[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

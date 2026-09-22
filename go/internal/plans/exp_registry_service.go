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

// WithTravelNorms подключает справочники командировок (суточные, нормативы
// перелёта, курс). Без них строка командировки сохраняется, но расчётные части
// остаются нулями — и это видно в самой строке, а не всплывает на своде.
func (s *ExpService) WithTravelNorms(n ExpTravelNorms) *ExpService { s.norms = n; return s }

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
	if r.Kind == ExpRegTravel {
		s.computeTravelRow(ctx, in, &r)
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

// computeTravelRow — расчётная часть строки командировки (находка Н3).
//
// Строка реестра командировок — это ОДНА поездка со своим месяцем, а не
// двенадцать месяцев, как у остальных реестров (ТЗ §5.2: колонка «Месяц» —
// поле ввода). Поэтому итог строки кладётся в месяц поездки, и только в него:
// так итог реестра по месяцам считается тем же суммированием, что у ФОТ и ИТ,
// без отдельной ветки.
//
// Нормативы читаются из справочников на каждом сохранении: пересчёт по старому
// нормативу выглядел бы правильным, а был бы неверным.
func (s *ExpService) computeTravelRow(ctx context.Context, in ExpInstance, r *ExpRegRow) {
	if r.Attrs == nil {
		r.Attrs = map[string]any{}
	}
	month := expAttrInt(r.Attrs, "month")
	if month < 1 || month > 12 {
		month = 1
	}
	country, _ := r.Attrs["country"].(string)
	city, _ := r.Attrs["city"].(string)

	calc := ExpTravelInput{
		Country: country, City: city,
		Days:    expAttrFloat(r.Attrs, "days"),
		Nights:  expAttrFloat(r.Attrs, "nights"),
		People:  expAttrFloat(r.Attrs, "people"),
		OneWay:  expAttrBool(r.Attrs, "one_way"),
		Lodging: expAttrFloat(r.Attrs, "lodging_rate"),
	}
	if s.norms != nil {
		if v, ok := s.norms.PerDiem(ctx, country, in.PlanYear); ok {
			calc.PerDiem = v
		}
		if v, ok := s.norms.FlightNorm(ctx, country, city); ok {
			calc.FlightBYN = v
		}
		if v, ok := s.norms.FxUSD(ctx, in.Currency, in.PlanYear, month); ok {
			calc.FxUSD = v
		}
	}
	got := ExpTravelCalc(calc)

	// Расчётные части кладём в атрибуты строки: их показывает форма и по ним
	// раскладывается публикация (билеты и командировочные — статья 27,
	// проживание — статья 30).
	r.Attrs["tickets"] = got.Tickets
	r.Attrs["lodging"] = got.Lodging
	r.Attrs["per_diem"] = got.PerDiem
	r.Attrs["total"] = got.Total
	r.Attrs["per_diem_norm"] = calc.PerDiem
	r.Attrs["flight_norm"] = calc.FlightBYN
	r.Attrs["fx_usd"] = calc.FxUSD
	r.CodePL = ExpPLTravel
	r.Months = map[int]float64{month: got.Total}
}

// expAttrFloat — числовой атрибут строки.
func expAttrFloat(attrs map[string]any, key string) float64 {
	switch v := attrs[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return 0
	}
}

// expAttrBool — флаг-атрибут строки.
func expAttrBool(attrs map[string]any, key string) bool {
	v, _ := attrs[key].(bool)
	return v
}

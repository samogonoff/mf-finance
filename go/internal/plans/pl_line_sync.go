package plans

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
)

// Синхронизация справочника статей PL из Budgeting.dbo.CodePL (правки ТЗ
// 02.09.2026, часть 5): 423 записи, десять колонок.
//
// Почему справочник, а не сид в коде: кроме подписи статьи он несёт НОРМАТИВНЫЙ
// НДС по пяти странам и статью ДДС, а это данные, которые меняются на стороне
// финблока. Сид PLLineSeed остаётся фолбэком для dev/тестов и для случая
// недоступного OLAP — он покрывает статьи формы МП и все статьи, на которых
// аналитик проверил трактовку НДС-колонок.
//
// Значения VAT_* переносятся КАК В ИСТОЧНИКЕ — коэффициентом «1 + ставка».
// Пересчёт в ставку делает RateBook.NormativeVat: если пересчитать при загрузке,
// сверка справочника с источником перестанет быть механической.

// PLLineTables — имена объектов источника (через конфиг, как у остальных).
type PLLineTables struct {
	CodePLTable string // Budgeting.dbo.CodePL
}

// PLLineProvider — провайдер dir_pl_line.
type PLLineProvider struct {
	db  *sql.DB
	tbl PLLineTables
}

// NewPLLineProvider — конструктор. db=nil → синхронизация недоступна, справочник
// остаётся на сиде (это не ошибка запуска: форма считает на seed-значениях).
func NewPLLineProvider(db *sql.DB, tbl PLLineTables) *PLLineProvider {
	return &PLLineProvider{db: db, tbl: tbl}
}

// Code — код справочника.
func (p *PLLineProvider) Code() string { return "dir_pl_line" }

// Fetch — все статьи справочника. Пустой результат приравниваем к ошибке:
// «справочник статей стал пустым» не должно молча вычистить строки, на которые
// ссылаются формы и маппинг публикации.
func (p *PLLineProvider) Fetch(ctx context.Context) ([]SyncRow, error) {
	if p.db == nil {
		return nil, errors.New("источник справочника статей (Budgeting.dbo.CodePL) не настроен")
	}
	if strings.TrimSpace(p.tbl.CodePLTable) == "" {
		return nil, errors.New("не задан PLANS_CODEPL_TABLE")
	}
	q := fmt.Sprintf(`
		SELECT [GroupPL], [CodePL], [Expense], [SpecialAccess], [CF_item],
		       [VAT_BY], [VAT_RU], [VAT_KZ], [VAT_UZ], [VAT_TR]
		  FROM %s`, p.tbl.CodePLTable)
	rows, err := p.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]SyncRow, 0, 450)
	skipped := 0
	for rows.Next() {
		var groupPL, codePL, expense, cfItem sql.NullString
		var special sql.NullInt64
		var vatBY, vatRU, vatKZ, vatUZ, vatTR sql.NullFloat64
		if err := rows.Scan(&groupPL, &codePL, &expense, &special, &cfItem,
			&vatBY, &vatRU, &vatKZ, &vatUZ, &vatTR); err != nil {
			return nil, err
		}
		// CodePL в источнике nvarchar. Нечисловые коды в справочнике встречаются
		// (технические строки) — форма ключуется числом, поэтому такие пропускаем,
		// но считаем и пишем в лог, а не теряем молча.
		code, cerr := strconv.Atoi(strings.TrimSpace(codePL.String))
		if !codePL.Valid || cerr != nil || code == 0 {
			skipped++
			continue
		}
		out = append(out, SyncRow{
			ExternalID: strconv.Itoa(code),
			Payload: map[string]any{
				"code_pl":        code,
				"expense_name":   strings.TrimSpace(expense.String),
				"group_pl":       strings.TrimSpace(groupPL.String),
				"cf_item":        strings.TrimSpace(cfItem.String),
				"special_access": special.Int64,
				"vat_by":         vatBY.Float64,
				"vat_ru":         vatRU.Float64,
				"vat_kz":         vatKZ.Float64,
				"vat_uz":         vatUZ.Float64,
				"vat_tr":         vatTR.Float64,
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, errors.New("справочник статей вернул 0 строк — синхронизация отменена")
	}
	if skipped > 0 {
		log.Printf("plans: справочник статей — пропущено строк с нечисловым CodePL: %d", skipped)
	}
	return out, nil
}

package debt

import "time"

// mockRows возвращает фикстурный набор строк отчёта.
// Покрывает edge-case'ы UI: 4 страны, 3 валюты, 1 партнёр с двумя договорами,
// смесь ДЗ/КЗ внутри одного субсчёта, строка с просрочкой и без.
func mockRows() []DebtRow {
	return []DebtRow{
		// ТД (РФ) ↔ Mark Formelle Kazakhstan — два договора, мульти-валюта
		{
			Country: CountryRF, Company: "ООО ТД «Марк Формэль»", CompanyINN: "6950135110",
			Partner: "ТОО Mark Formelle Kazakhstan", PartnerINN: "141240004842",
			Account: "60", AccountName: "Расчёты с поставщиками и подрядчиками",
			Subaccount: "60.01", SubaccountName: "Расчёты с поставщиками и подрядчиками",
			Contract: "ДП-2024/03 от 12.03.2024", PaymentTermDays: 30, Currency: "RUB",
			OpeningDZ: 0, OpeningKZ: 1_245_300.00,
			TurnoverDZ: 50_000, TurnoverKZ: 880_000.00,
			ClosingDZ: 0, ClosingKZ: 2_075_300.00,
			RevenuePeriod: 0, RevenueLastMonth: 0,
		},
		{
			Country: CountryRF, Company: "ООО ТД «Марк Формэль»", CompanyINN: "6950135110",
			Partner: "ТОО Mark Formelle Kazakhstan", PartnerINN: "141240004842",
			Account: "60", AccountName: "Расчёты с поставщиками и подрядчиками",
			Subaccount: "60.01", SubaccountName: "Расчёты с поставщиками и подрядчиками",
			Contract: "ДП-2024/03 от 12.03.2024", PaymentTermDays: 30, Currency: "USD",
			OpeningDZ: 0, OpeningKZ: 12_400.50,
			TurnoverDZ: 0, TurnoverKZ: 5_300.00,
			ClosingDZ: 0, ClosingKZ: 17_700.50,
			RevenuePeriod: 0, RevenueLastMonth: 0,
		},
		{
			Country: CountryRF, Company: "ООО ТД «Марк Формэль»", CompanyINN: "6950135110",
			Partner: "ТОО Mark Formelle Kazakhstan", PartnerINN: "141240004842",
			Account: "62", AccountName: "Расчёты с покупателями и заказчиками",
			Subaccount: "62.01", SubaccountName: "Расчёты с покупателями и заказчиками",
			Contract: "ОТГ-2025/11 от 04.11.2025", PaymentTermDays: 45, Currency: "RUB",
			OpeningDZ: 3_120_000.00, OpeningKZ: 0,
			TurnoverDZ: 1_800_000.00, TurnoverKZ: 1_200_000.00,
			ClosingDZ: 3_720_000.00, ClosingKZ: 0,
			RevenuePeriod: 1_800_000.00, RevenueLastMonth: 420_000.00,
		},
		// РБ → УЗ, два юрлица одного партнёра
		{
			Country: CountryRB, Company: "ООО «Марк Формэль»", CompanyINN: "690591512",
			Partner: "ООО «MARK FORMELLE IT» МЧЖ", PartnerINN: "305554644",
			Account: "62", AccountName: "Расчёты с покупателями и заказчиками",
			Subaccount: "62.1", SubaccountName: "Расчёты по реализации",
			Contract: "ВНЕШ-2025/02 от 18.02.2025", PaymentTermDays: 60, Currency: "BYN",
			OpeningDZ: 18_400.00, OpeningKZ: 0,
			TurnoverDZ: 9_200.00, TurnoverKZ: 5_100.00,
			ClosingDZ: 22_500.00, ClosingKZ: 0,
			RevenuePeriod: 9_200.00, RevenueLastMonth: 3_400.00,
		},
		// смесь ДЗ и КЗ в рамках одного субсчёта — проверка знаковой раскраски
		{
			Country: CountryRB, Company: "ООО «Марк Формэль»", CompanyINN: "690591512",
			Partner: "ООО «Формэль»", PartnerINN: "690719790",
			Account: "76", AccountName: "Расчёты с разными дебиторами и кредиторами",
			Subaccount: "76.05", SubaccountName: "Расчёты с прочими поставщиками и подрядчиками",
			Contract: "ВЗ-2024/К-10-1873/А от 20.06.2022", PaymentTermDays: 30, Currency: "BYN",
			OpeningDZ: 4_300.00, OpeningKZ: 1_800.00,
			TurnoverDZ: 2_100.00, TurnoverKZ: 950.00,
			ClosingDZ: 5_450.00, ClosingKZ: 1_800.00,
			RevenuePeriod: 0, RevenueLastMonth: 0,
		},
		// КЗ → РБ, выручка из Дт 1210 / Кт 6010
		{
			Country: CountryKZ, Company: "ТОО Mark Formelle Kazakhstan", CompanyINN: "141240004842",
			Partner: "ООО «Марк Формэль»", PartnerINN: "690591512",
			Account: "1210", AccountName: "Краткосрочная дебиторская задолженность покупателей и заказчиков",
			Subaccount: "1210", SubaccountName: "Краткосрочная дебиторская задолженность",
			Contract: "ИМП-2024/04 от 10.04.2024", PaymentTermDays: 90, Currency: "USD",
			OpeningDZ: 220_000.00, OpeningKZ: 0,
			TurnoverDZ: 84_000.00, TurnoverKZ: 60_000.00,
			ClosingDZ: 244_000.00, ClosingKZ: 0,
			RevenuePeriod: 84_000.00, RevenueLastMonth: 22_000.00,
		},
		// УЗ → РФ, без срока оплаты — overdue=0 на drill-down
		{
			Country: CountryUZ, Company: "ООО «MARK FORMELLE IT» МЧЖ", CompanyINN: "305554644",
			Partner: "ООО ТД «Марк Формэль»", PartnerINN: "6950135110",
			Account: "6000", AccountName: "Расчёты с поставщиками и подрядчиками",
			Subaccount: "6010", SubaccountName: "Расчёты с поставщиками",
			Contract: "СНБ-2025/07 от 14.07.2025", PaymentTermDays: 0, Currency: "USD",
			OpeningDZ: 0, OpeningKZ: 9_800.00,
			TurnoverDZ: 0, TurnoverKZ: 3_200.00,
			ClosingDZ: 0, ClosingKZ: 13_000.00,
			RevenuePeriod: 0, RevenueLastMonth: 0,
		},
	}
}

// mockDrilldown возвращает фикстурный набор документов для (company, partner, account, contract, currency).
// Содержит как просроченные документы (overdue_days > 0), так и в пределах срока (overdue_days = 0).
func mockDrilldown(contract, currency string, reportDate time.Time) []DocumentRow {
	if contract == "ДП-2024/03 от 12.03.2024" && currency == "RUB" {
		d1 := time.Date(2025, 11, 5, 0, 0, 0, 0, time.UTC)
		d2 := time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)
		due1 := d1.AddDate(0, 0, 30)
		due2 := d2.AddDate(0, 0, 30)
		return []DocumentRow{
			{
				DocDate: d1, DocNumber: "ПТ-001234", DocKind: "Поступление товаров",
				DZChange: 0, KZChange: 580_000.00,
				PaymentDueDate: due1,
				OverdueDays:    daysOverdue(due1, reportDate),
			},
			{
				DocDate: d2, DocNumber: "ПТ-001891", DocKind: "Поступление товаров",
				DZChange: 0, KZChange: 300_000.00,
				PaymentDueDate: due2,
				OverdueDays:    daysOverdue(due2, reportDate),
			},
		}
	}
	if contract == "ОТГ-2025/11 от 04.11.2025" && currency == "RUB" {
		d1 := time.Date(2025, 12, 20, 0, 0, 0, 0, time.UTC)
		d2 := time.Date(2026, 3, 18, 0, 0, 0, 0, time.UTC)
		due1 := d1.AddDate(0, 0, 45)
		due2 := d2.AddDate(0, 0, 45)
		return []DocumentRow{
			{
				DocDate: d1, DocNumber: "РТ-005678", DocKind: "Реализация товаров",
				DZChange: 980_000.00, KZChange: 0,
				PaymentDueDate: due1,
				OverdueDays:    daysOverdue(due1, reportDate),
			},
			{
				DocDate: d2, DocNumber: "РТ-006012", DocKind: "Реализация товаров",
				DZChange: 820_000.00, KZChange: 0,
				PaymentDueDate: due2,
				OverdueDays:    daysOverdue(due2, reportDate),
			},
		}
	}
	// fallback: один документ без просрочки
	d := time.Date(2026, 4, 15, 0, 0, 0, 0, time.UTC)
	due := d.AddDate(0, 0, 60)
	return []DocumentRow{
		{
			DocDate: d, DocNumber: "—", DocKind: "Документ операции",
			DZChange: 0, KZChange: 0,
			PaymentDueDate: due,
			OverdueDays:    daysOverdue(due, reportDate),
		},
	}
}

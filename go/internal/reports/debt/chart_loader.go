package debt

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// MatrixTables — имена таблиц P&L-матриц на OLAP (схема dbo БД FinDWH).
// В отличие от PremasterTables эти имена содержат пробелы и начинаются с цифр
// ("001 Mapping PL by BK"), поэтому квотируются в [скобках] и валидируются
// мягче (запрещён только ']' и перевод строки — защита от инъекции через env).
type MatrixTables struct {
	Database  string // FinDWH
	Schema    string // dbo
	MappingPL string // 001 Mapping PL by BK — (Company, DrAcc, CrAcc) → Mapping(=CodePL)
	CodePL    string // 002 CodePL — CodePL → GroupPL ('ПРОДАЖИ' = выручка)
	Companies string // CompaniesMF — Company → Country (маппинг в SQL, не в Go)
}

// bracketIdent квотирует идентификатор в [скобки], отвергая ']' и переводы строк.
func bracketIdent(kind, v string) (string, error) {
	if v == "" || strings.ContainsAny(v, "]\r\n") {
		return "", fmt.Errorf("debt.MatrixTables: invalid %s %q", kind, v)
	}
	return "[" + v + "]", nil
}

// fqn собирает [Database].[Schema].[table] с валидацией каждого сегмента.
func (t MatrixTables) fqn(table string) (string, error) {
	dbp, err := bracketIdent("database", t.Database)
	if err != nil {
		return "", err
	}
	sp, err := bracketIdent("schema", t.Schema)
	if err != nil {
		return "", err
	}
	tp, err := bracketIdent("table", table)
	if err != nil {
		return "", err
	}
	return dbp + "." + sp + "." + tp, nil
}

// LoadRevenueOverlay читает из матрицы [001 Mapping PL by BK] корни счетов,
// которые сворачиваются в статью P&L с GroupPL='ПРОДАЖИ', сгруппированные по
// стране юрлица. Результат скармливается InstallRevenueOverlay — он помечает
// эти счета KindRevenue, не трогая ДЗ/КЗ.
//
// ЧТО ЭТО ЗАКРЫВАЕТ: вопрос №4 (какие именно субсчета — выручка) точно и для
// всех стран, присутствующих в матрице. НЕ закрывает ДЗ/КЗ-баланс по 5 новым
// странам — балансовой природы счёта в P&L-матрице нет (schema-draft §8a.4).
//
// ДОПУЩЕНИЯ (проверить на живой БД, при расхождении — поправить здесь):
//  1. Колонки [001 Mapping PL by BK]: Company, CrAcc, Mapping; Mapping = код CodePL.
//  2. Выручка определяется по [002 CodePL].GroupPL = N'ПРОДАЖИ'.
//  3. Счёт выручки — это КРЕДИТ проводки реализации (CrAcc). Dr — контрсчёт (62/«дебиторка»).
//  4. Страна берётся из [CompaniesMF].Country по совпадению имени компании
//     ([CompaniesMF].CompanyMFName1C = [Mapping PL by BK].Company). Если в матрице
//     Company хранит УНП — поменять join на cmf.UNP = m.Company.
//
// Любая ошибка возвращается наверх — вызывающий код (main.go) её логирует и
// оставляет хардкод-классификацию (фолбэк), сервис не валится.
func LoadRevenueOverlay(ctx context.Context, db *sql.DB, t MatrixTables) (map[Country][]string, error) {
	mappingFQN, err := t.fqn(t.MappingPL)
	if err != nil {
		return nil, err
	}
	codePLFQN, err := t.fqn(t.CodePL)
	if err != nil {
		return nil, err
	}
	companiesFQN, err := t.fqn(t.Companies)
	if err != nil {
		return nil, err
	}

	// Корень счёта — как в repo_premaster: LEFT(acc, CHARINDEX('.', acc+'.')-1).
	q := fmt.Sprintf(`
SELECT DISTINCT
    cmf.Country AS country,
    LEFT(m.CrAcc, CHARINDEX('.', m.CrAcc + '.') - 1) AS acc_root
FROM %[1]s m WITH (NOLOCK)
JOIN %[2]s p   WITH (NOLOCK) ON p.CodePL = m.Mapping
JOIN %[3]s cmf WITH (NOLOCK) ON cmf.CompanyMFName1C = m.Company
WHERE p.GroupPL = N'ПРОДАЖИ'
  AND m.CrAcc IS NOT NULL AND LTRIM(RTRIM(m.CrAcc)) <> ''`, mappingFQN, codePLFQN, companiesFQN)

	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("debt.LoadRevenueOverlay: query: %w", err)
	}
	defer rows.Close()

	overlay := map[Country][]string{}
	seen := map[Country]map[string]struct{}{}
	for rows.Next() {
		var rawCountry, accRoot string
		if err := rows.Scan(&rawCountry, &accRoot); err != nil {
			return nil, fmt.Errorf("debt.LoadRevenueOverlay: scan: %w", err)
		}
		country, ok := normalizeCountry(rawCountry)
		if !ok {
			continue // страна не из нашего ГК-перечня — пропускаем
		}
		accRoot = strings.TrimSpace(accRoot)
		if accRoot == "" {
			continue
		}
		if seen[country] == nil {
			seen[country] = map[string]struct{}{}
		}
		if _, dup := seen[country][accRoot]; dup {
			continue
		}
		seen[country][accRoot] = struct{}{}
		overlay[country] = append(overlay[country], accRoot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("debt.LoadRevenueOverlay: rows: %w", err)
	}
	return overlay, nil
}

// normalizeCountry сопоставляет строку Country из CompaniesMF нашему типу Country.
// Неизвестные значения отбрасываются (false) — чтобы случайный мусор из витрины
// не создавал «страну-призрак» в классификации.
func normalizeCountry(s string) (Country, bool) {
	c := Country(strings.TrimSpace(s))
	switch c {
	case CountryRB, CountryRF, CountryKZ, CountryUZ,
		CountryTR, CountryCZ, CountryGB, CountryCN, CountryKG:
		return c, true
	default:
		return "", false
	}
}

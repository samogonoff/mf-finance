package etl

import "database/sql"

// CodeByINN — ИНН/УНП → короткий код компании GLMF.Company. Кластерный индекс
// GLMF = (Company, DrAcc, CrAcc, Date), поэтому фильтр по Company (коду) идёт
// через seek, а по CompanyID (ИНН) — full scan 209M строк. Источник кодов —
// CompaniesMF (снято с OLAP). Дубликат debt-справочника (etl не импортит debt).
var CodeByINN = map[string]string{
	"690591512":    "MF",
	"690719790":    "F",
	"6950135110":   "TDMF",
	"5031159833":   "MFTex",
	"9909349268":   "MFT",
	"9731039708":   "PTIR",
	"141240004842": "MFKaz",
	"305554644":    "MFUz",
	"310170662":    "BR",
	"692221084":    "DR",
	"693335015":    "DR2",
	"190465888":    "GP",
}

// glmfCompanyWhere — фильтр компании для GLMF-запросов. Если код известен —
// `p.Company = @co` (seek по кластерному индексу) + `p.CompanyID = @inn` (residual);
// иначе фолбэк на `p.CompanyID = @inn` (медленно, full scan — но корректно).
func glmfCompanyWhere(inn string) (string, []interface{}) {
	if code, ok := CodeByINN[inn]; ok {
		return "p.Company = @co AND p.CompanyID = @inn",
			[]interface{}{sql.Named("co", code), sql.Named("inn", inn)}
	}
	return "p.CompanyID = @inn", []interface{}{sql.Named("inn", inn)}
}

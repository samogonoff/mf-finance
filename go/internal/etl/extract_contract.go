package etl

import (
	"fmt"
	"regexp"
	"strings"
)

// Extract субконто Premaster → finance.dim_contract (поток 2). GLMF не несёт
// договор (субконто нет), поэтому отдельный поток: doc_id → название договора
// из субконто Premaster1C(+History) ⋈ Objects. Грануляр. = 1 договор на DocID
// (проверено). Связь с fact_glmf по doc_id (идентичны). SPEC §4, probe-contracts.md.
//
// Позиция субконто зависит от счёта (probe-contracts.md): 62 (ДЗ) → DrSubconto2;
// 60/76 (КЗ) → CrSubconto1. КЗ/УЗ-счета — отдельной задачей (T8). Сырой резолв
// даёт мусор (документы/контрагенты) → эвристика-фильтр isContractName.

// contractRow — строка dim_contract (JSONEachRow).
type contractRow struct {
	DocID        string `json:"doc_id"`
	CompanyID    string `json:"company_id"` // ИНН ЮЛ — для счётчика договоров per-company
	ContractRef  string `json:"contract_ref"`
	ContractName string `json:"contract_name"`
	AccountKind  string `json:"account_kind"` // '62' | '60' | '76' | КЗ/УЗ-счёт
}

// reContractKeep/reContractDrop — эвристика «название похоже на договор».
var (
	reContractKeep = regexp.MustCompile(`(?i)договор|соглашен|оферт|контракт|№|\d+/\d+|от \d{2}\.\d{2}\.\d{4}`)
	// «без догов» (КЗ-литерал «Без Договора») отсекаем ПЕРВЫМ, иначе reContractKeep
	// поймает слово «договор» в нём.
	reContractDrop = regexp.MustCompile(`(?i)без догов|00БС|ТДБП|оказание|реализаци|поступлени|накладн`)
)

// isContractName — эвристика: оставлять ли резолвленное имя субконто как договор.
// Сначала отсекаем явные документы (00БС/ТДБП/Реализация/Поступление/Накладная),
// затем оставляем только похожее на договор (Договор/Соглашение/Оферта/№/N/N/дата).
func isContractName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if reContractDrop.MatchString(name) {
		return false
	}
	return reContractKeep.MatchString(name)
}

// extractContractSelectFrom — DISTINCT (DocID, субконто-ссылка, Objects.Name, kind)
// по строкам со счётом 62/60/76. table — FQN Premaster1C или Premaster1CHistory.
// Эвристика-фильтр имени применяется в Go (isContractName) — здесь только
// предотсев очевидного мусора NOT LIKE.
func extractContractSelectFrom(table string) string {
	dr := accRootSQL("p.DrAcc")
	cr := accRootSQL("p.CrAcc")
	// sub1Roots — счета, у которых договор в Subconto1 на стороне счёта:
	// 60/76 (РФ/РБ КЗ), 1210 (КЗ ДЗ), 3310/3510 (КЗ КЗ), 4000/4010/4090/4300/4800 (УЗ ДЗ),
	// 6000/6300/6910 (УЗ КЗ). 62 — ИСКЛЮЧЕНИЕ (договор в Subconto2; Subconto1=контрагент).
	sub1 := "'60','76','1210','3310','3510','4000','4010','4090','4300','4800','6000','6300','6910'"
	ref := fmt.Sprintf(`CASE
	    WHEN %[1]s = '62' THEN NULLIF(p.DrSubconto2,'')
	    WHEN %[2]s = '62' THEN NULLIF(p.CrSubconto2,'')
	    WHEN %[1]s IN (%[3]s) THEN NULLIF(p.DrSubconto1,'')
	    WHEN %[2]s IN (%[3]s) THEN NULLIF(p.CrSubconto1,'')
	  END`, dr, cr, sub1)
	kind := fmt.Sprintf(`CASE
	    WHEN %[1]s = '62' OR %[2]s = '62' THEN '62'
	    WHEN %[1]s IN (%[3]s) THEN %[1]s
	    WHEN %[2]s IN (%[3]s) THEN %[2]s
	  END`, dr, cr, sub1)
	return `
SELECT DISTINCT
    ISNULL(CONVERT(NVARCHAR(MAX), p.DocID), '') AS doc_id,
    ISNULL(p.CompanyID, '')                     AS company_id,
    cc.ref                                       AS contract_ref,
    ISNULL(o.[Name], '')                         AS contract_name,
    cc.kind                                      AS account_kind
FROM ` + table + ` AS p WITH (NOLOCK)
CROSS APPLY (SELECT ` + ref + ` AS ref, ` + kind + ` AS kind) cc
LEFT JOIN [FinDWH].[dbo].[Objects] AS o WITH (NOLOCK) ON o.ID = cc.ref
WHERE cc.ref IS NOT NULL
  AND ISNULL(o.[Name],'') <> ''
  AND o.[Name] NOT LIKE '%00БС%' AND o.[Name] NOT LIKE '%ТДБП%'
  AND o.[Name] NOT LIKE 'Поступлени%' AND o.[Name] NOT LIKE 'Реализаци%'`
}

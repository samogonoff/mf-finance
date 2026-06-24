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
	ContractRef  string `json:"contract_ref"`
	ContractName string `json:"contract_name"`
	AccountKind  string `json:"account_kind"` // '62' | '60' | '76'
}

// reContractKeep/reContractDrop — эвристика «название похоже на договор».
var (
	reContractKeep = regexp.MustCompile(`(?i)договор|соглашен|оферт|контракт|№|\d+/\d+|от \d{2}\.\d{2}\.\d{4}`)
	reContractDrop = regexp.MustCompile(`(?i)00БС|ТДБП|оказание|реализаци|поступлени|накладн`)
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
	// ref/kind: 62→DrSubconto2/CrSubconto2; 60/76→CrSubconto1/DrSubconto1.
	ref := fmt.Sprintf(`CASE
	    WHEN %[1]s = '62' THEN NULLIF(p.DrSubconto2,'')
	    WHEN %[2]s = '62' THEN NULLIF(p.CrSubconto2,'')
	    WHEN %[2]s IN ('60','76') THEN NULLIF(p.CrSubconto1,'')
	    WHEN %[1]s IN ('60','76') THEN NULLIF(p.DrSubconto1,'')
	  END`, dr, cr)
	kind := fmt.Sprintf(`CASE
	    WHEN %[1]s = '62' OR %[2]s = '62' THEN '62'
	    WHEN %[2]s = '60' OR %[1]s = '60' THEN '60'
	    WHEN %[2]s = '76' OR %[1]s = '76' THEN '76'
	  END`, dr, cr)
	return `
SELECT DISTINCT
    CONVERT(NVARCHAR(MAX), p.DocID) AS doc_id,
    cc.ref                          AS contract_ref,
    ISNULL(o.[Name], '')            AS contract_name,
    cc.kind                         AS account_kind
FROM ` + table + ` AS p WITH (NOLOCK)
CROSS APPLY (SELECT ` + ref + ` AS ref, ` + kind + ` AS kind) cc
LEFT JOIN [FinDWH].[dbo].[Objects] AS o WITH (NOLOCK) ON o.ID = cc.ref
WHERE cc.ref IS NOT NULL
  AND ISNULL(o.[Name],'') <> ''
  AND o.[Name] NOT LIKE '%00БС%' AND o.[Name] NOT LIKE '%ТДБП%'
  AND o.[Name] NOT LIKE 'Поступлени%' AND o.[Name] NOT LIKE 'Реализаци%'`
}

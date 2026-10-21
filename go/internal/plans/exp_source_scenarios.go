package plans

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// Диагностика сценариев источника расходов: какие сценарии реально лежат в реплике P&L (finance.findwh_table_fin_pl) и в
// снимке процедуры затрат (finance.exp_costs), и находятся ли среди них названия, под которыми мы читаем факт, стратегию
// и тактику. Названия сценариев в Table_Fin_PL — предположение, и проверяется оно этим отчётом, а не догадкой.

// ExpScenarioCount — сценарий источника: сколько строк и за какие месяцы.
type ExpScenarioCount struct {
	Scenario  string `json:"scenario"`
	ForReport string `json:"for_report,omitempty"`
	Rows      int64  `json:"rows"`
	FromMonth string `json:"from_month"`
	ToMonth   string `json:"to_month"`
}

// ExpSourceScenarios — сценарии источников и настроенные названия.
type ExpSourceScenarios struct {
	FinPL     []ExpScenarioCount `json:"finpl"`
	Procedure []ExpScenarioCount `json:"procedure"`
	// Configured — какие названия сценариев ищем в реплике P&L; Missing — те из них, которых в реплике нет вовсе;
	// WrongReport — которые есть, но не со значением «Для отчета», по которому читает рабочий запрос (PL, BS_20, BS_25):
	// форма их не подхватит.
	Configured  map[string][]string `json:"configured"`
	Missing     map[string][]string `json:"missing"`
	WrongReport map[string][]string `json:"wrong_report"`
}

// ExpScenarioLister — источник умеет рассказать о своих сценариях (читатель из ClickHouse).
type ExpScenarioLister interface {
	SourceScenarios(ctx context.Context) (ExpSourceScenarios, error)
}

type chScenarioRow struct {
	Scenario  string      `json:"scenario"`
	ForReport string      `json:"for_report"`
	Rows      json.Number `json:"rows"` // UInt64 приходит строкой или числом - зависит от настроек ClickHouse
	FromMonth string      `json:"from_month"`
	ToMonth   string      `json:"to_month"`
}

func (s *chExpSource) scenarioRows(ctx context.Context, sql string) ([]ExpScenarioCount, error) {
	body, err := s.query(ctx, sql, "JSONEachRow")
	if err != nil {
		return nil, err
	}
	var out []ExpScenarioCount
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r chScenarioRow
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, err
		}
		n, _ := r.Rows.Int64()
		out = append(out, ExpScenarioCount{Scenario: r.Scenario, ForReport: r.ForReport, Rows: n, FromMonth: r.FromMonth, ToMonth: r.ToMonth})
	}
	return out, nil
}

// SourceScenarios читает сценарии реплики P&L и снимка процедуры затрат.
func (s *chExpSource) SourceScenarios(ctx context.Context) (ExpSourceScenarios, error) {
	out := ExpSourceScenarios{Configured: map[string][]string{}, Missing: map[string][]string{}, WrongReport: map[string][]string{}}
	fin, err := s.scenarioRows(ctx, `SELECT scenario, for_report, count() AS rows, toString(min(month)) AS from_month, toString(max(month)) AS to_month
  FROM finance.findwh_table_fin_pl GROUP BY scenario, for_report ORDER BY scenario, for_report`)
	if err != nil {
		return out, err
	}
	proc, err := s.scenarioRows(ctx, `SELECT scenario, '' AS for_report, count() AS rows, toString(min(month)) AS from_month, toString(max(month)) AS to_month
  FROM finance.exp_costs GROUP BY scenario ORDER BY scenario`)
	if err != nil {
		return out, err
	}
	out.FinPL, out.Procedure = fin, proc
	// Как в рабочем запросе: без обрезки пробелов (ETL реплики строки не обрезает), «Для отчета» только из expFinPLReports.
	any, usable := map[string]bool{}, map[string]bool{}
	for _, f := range fin {
		name := strings.ToLower(f.Scenario)
		any[name] = true
		for _, r := range expFinPLReports {
			if f.ForReport == r {
				usable[name] = true
			}
		}
	}
	for key, names := range map[string][]string{"fact": {"факт"}, "strategy": s.finPLStrategy, "tactic": s.finPLTactic} {
		out.Configured[key] = names
		for _, n := range names {
			switch name := strings.ToLower(n); {
			case usable[name]:
			case any[name]:
				out.WrongReport[key] = append(out.WrongReport[key], n)
			default:
				out.Missing[key] = append(out.Missing[key], n)
			}
		}
	}
	return out, nil
}

// SourceScenarios — сценарии источника расходов (диагностика для администратора).
func (s *ExpService) SourceScenarios(ctx context.Context) (ExpSourceScenarios, error) {
	l, ok := s.source.(ExpScenarioLister)
	if !ok {
		return ExpSourceScenarios{}, errors.New("источник расходов не ClickHouse: сценарии показать нельзя")
	}
	return l.SourceScenarios(ctx)
}

// SourceScenariosGet — GET /api/plans/exp-source/scenarios (администратор процессов).
func (h *ExpHandler) SourceScenariosGet(w http.ResponseWriter, r *http.Request) {
	res, err := h.svc.SourceScenarios(r.Context())
	if err != nil {
		expErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

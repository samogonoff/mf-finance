package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
)

type taskDataObject struct {
	Country     string
	LegalEntity string
}

// taskDataObjects resolves country and legal entity from the form directory,
// then the common CFO directory. A missing object is not authorized merely because a user has a
// broad rule. Marketplace seed is the established fallback for unsynced MP CFOs.
func (s *TaskStore) taskDataObjects(ctx context.Context, formCode string, codes []int) (map[int]taskDataObject, error) {
	out := map[int]taskDataObject{}
	if len(codes) == 0 {
		return out, nil
	}
	keys := make([]string, 0, len(codes))
	for _, c := range codes {
		keys = append(keys, strconv.Itoa(c))
	}
	directories := []string{"dir_cfo"}
	if formCode == TemplateMP {
		directories = append(directories, "dir_marketplace")
	}
	if formCode == TemplateRetail {
		directories = append(directories, "dir_retail_store")
	}
	rows, err := s.pool.Query(ctx, `SELECT d.code, r.external_id, r.payload_json
        FROM plans_directory_row r JOIN plans_directory d ON d.id=r.directory_id
		WHERE d.code=ANY($1) AND (r.external_id = ANY($2) OR r.payload_json->>'code_cfo' = ANY($2))`, directories, keys)
	if err != nil {
		return nil, err
	}
	conflicted := map[int]bool{}
	priority := map[int]int{}
	for rows.Next() {
		var directory, key string
		var raw []byte
		if err := rows.Scan(&directory, &key, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var obj struct {
			CodeCFO     json.RawMessage `json:"code_cfo"`
			Country     string          `json:"country"`
			LegalEntity string          `json:"legal_entity"`
		}
		if err := json.Unmarshal(raw, &obj); err != nil {
			rows.Close()
			return nil, err
		}
		code, ok := parseNumericCFO(key)
		if !ok {
			var numeric int
			if json.Unmarshal(obj.CodeCFO, &numeric) == nil {
				code, ok = numeric, true
			}
			if !ok {
				var codeText string
				if json.Unmarshal(obj.CodeCFO, &codeText) == nil {
					code, ok = parseNumericCFO(codeText)
				}
			}
		}
		if !ok || code <= 0 {
			continue
		}
		next := taskDataObject{Country: obj.Country, LegalEntity: obj.LegalEntity}
		level := 1
		if directory != "dir_cfo" {
			level = 2
		}
		if level < priority[code] {
			continue
		}
		if level > priority[code] {
			conflicted[code] = false
		}
		if prev, ok := out[code]; ok && level == priority[code] && prev != next {
			conflicted[code] = true
		}
		out[code] = next
		priority[code] = level
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for c, bad := range conflicted {
		if bad {
			delete(out, c)
		}
	}
	if formCode == TemplateMP {
		for _, p := range MarketplaceSeed() {
			if _, exists := out[p.CodeCFO]; !exists && !conflicted[p.CodeCFO] {
				out[p.CodeCFO] = taskDataObject{Country: p.Country, LegalEntity: p.LegalEntity}
			}
		}
	}
	return out, nil
}

func taskDataRuleAllows(rule UserScope, code int, obj taskDataObject) bool {
	if rule.Country != "" && obj.Country == "" {
		return false
	}
	if rule.LegalEntity != "" && obj.LegalEntity == "" {
		return false
	}
	return ruleAllows(rule, code, obj.Country, obj.LegalEntity)
}

func filterTaskDataByScope(data TaskData, objects map[int]taskDataObject, scope ScopeFilter) (TaskData, error) {
	if scope.Admin {
		return data, nil
	}
	codes := map[int]bool{}
	for _, c := range data.Task.CfoCodes {
		if c > 0 {
			codes[c] = true
		}
	}
	for _, r := range data.Rows {
		if r.CodeCFO > 0 {
			codes[r.CodeCFO] = true
		}
	}
	allowed := map[int]bool{}
	for code := range codes {
		obj, ok := objects[code]
		if !ok {
			continue
		}
		for _, rule := range scope.Rules {
			if taskDataRuleAllows(rule, code, obj) {
				allowed[code] = true
				break
			}
		}
	}
	if len(allowed) == 0 {
		return TaskData{}, fmt.Errorf("задание вне вашего среза доступа")
	}
	all := len(allowed) == len(codes)
	rows := make([]TaskDataRow, 0, len(data.Rows))
	for _, row := range data.Rows {
		if allowed[row.CodeCFO] || row.CodeCFO == 0 && all {
			rows = append(rows, row)
		}
	}
	data.Rows = rows
	data.HasData = len(rows) > 0
	visibleCodes := make([]int, 0, len(codes))
	for code := range codes {
		if allowed[code] {
			visibleCodes = append(visibleCodes, code)
		}
	}
	// Stable order makes UI and audit output deterministic.
	sort.Ints(visibleCodes)
	data.Task.CfoCodes = visibleCodes
	data.Task.CfoCount = len(visibleCodes)
	return data, nil
}

func (h *Handler) scopedTaskData(r *http.Request, data TaskData) (TaskData, error) {
	p, ok := h.principal(r)
	if !ok {
		return TaskData{}, fmt.Errorf("пользователь не определён")
	}
	if p.PlansAdmin {
		return data, nil
	}
	if h.form == nil || h.form.scope == nil {
		return TaskData{}, fmt.Errorf("срез доступа недоступен")
	}
	scope, err := scopeFilterFor(r.Context(), h.form.scope, p)
	if err != nil {
		return TaskData{}, fmt.Errorf("срез доступа недоступен: %w", err)
	}
	codes := make([]int, 0, len(data.Task.CfoCodes)+len(data.Rows))
	codes = append(codes, data.Task.CfoCodes...)
	for _, row := range data.Rows {
		codes = append(codes, row.CodeCFO)
	}
	objects, err := h.tasks.taskDataObjects(r.Context(), data.Task.FormCode, codes)
	if err != nil {
		return TaskData{}, fmt.Errorf("справочник ЦФО недоступен: %w", err)
	}
	return filterTaskDataByScope(data, objects, scope)
}

package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// cardStepOwnerResolver supplements period owners with the scoped assignee of a
// form. In particular fin has no period stage, so a stage-only resolver silently
// dropped notifications addressed to the financial controller.
type cardStepOwnerResolver struct {
	pool     *pgxpool.Pool
	fallback StepOwnerResolver
}

func NewCardStepOwnerResolver(pool *pgxpool.Pool, tasks stageOwnerReader) StepOwnerResolver {
	return &cardStepOwnerResolver{pool: pool, fallback: NewStageOwnerResolver(tasks)}
}

func (r *cardStepOwnerResolver) StepOwners(ctx context.Context, plID int64, formCode, scopeKey string) (map[string]int64, error) {
	owners, err := r.fallback.StepOwners(ctx, plID, formCode, scopeKey)
	if err != nil || r.pool == nil {
		return owners, err
	}
	card, err := NewCardStore(r.pool).CardByScope(ctx, plID, formCode, scopeKey)
	if err != nil {
		return nil, err
	}
	// Read explicit assignments only: an observer with unrestricted stage access
	// is not the person assigned to every step of every card.
	rows, err := r.pool.Query(ctx, `SELECT s.user_id, s.role, s.stage_code, s.country, s.legal_entity, s.code_cfo
 FROM plans_user_scope s JOIN users u ON u.id=s.user_id
 JOIN plans_form_route route ON route.form_code=$1 AND route.step_code=s.stage_code
 WHERE route.enabled AND route.kind <> 'fill' AND NOT u.is_blocked`, formCode)
	if err != nil {
		return nil, err
	}
	var assignments []UserScope
	for rows.Next() {
		var a UserScope
		var raw []byte
		if err := rows.Scan(&a.UserID, &a.Role, &a.StageCode, &a.Country, &a.LegalEntity, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(raw, &a.CodeCFO); err != nil {
			rows.Close()
			return nil, err
		}
		assignments = append(assignments, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Country/legal entity/CFO belong to one rule; never combine the country of
	// one assignment with the CFO list of another.
	plats, err := r.pool.Query(ctx, `SELECT r.payload_json FROM plans_directory_row r
 JOIN plans_directory d ON d.id=r.directory_id WHERE d.code=$1`, ownerDirectory(formCode))
	if err != nil {
		return nil, err
	}
	var objects []MpFormPlatform
	for plats.Next() {
		var raw []byte
		if err := plats.Scan(&raw); err != nil {
			plats.Close()
			return nil, err
		}
		var p struct {
			CodeCFO     json.RawMessage `json:"code_cfo"`
			Country     string          `json:"country"`
			LegalEntity string          `json:"legal_entity"`
			Segment     string          `json:"segment"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			plats.Close()
			return nil, err
		}
		var code int
		if err := json.Unmarshal(p.CodeCFO, &code); err != nil {
			var str string
			_ = json.Unmarshal(p.CodeCFO, &str)
			code, _ = parseNumericCFO(str)
		}
		if code == 0 {
			continue
		}
		if formCode == TemplateMP && p.Segment != scopeKey {
			continue
		}
		if formCode == TemplateRetail && !strings.EqualFold(p.Country, card.Country) {
			continue
		}
		if p.LegalEntity == "" && formCode == TemplateRetail {
			p.LegalEntity = card.LegalEntity
		}
		objects = append(objects, MpFormPlatform{CodeCFO: code, Country: p.Country, LegalEntity: p.LegalEntity, Segment: p.Segment})
	}
	plats.Close()
	if err := plats.Err(); err != nil {
		return nil, err
	}
	owners, err = resolveScopedStepOwners(owners, card, objects, assignments)
	if err != nil {
		return nil, err
	}
	// The filler is the assignee/delegate of the task for this card, not the
	// administrator who created the whole period or its generic stage owner.
	taskRows, err := r.pool.Query(ctx, `SELECT COALESCE(t.delegate_user_id,t.assignee_user_id,0), COALESCE(tpl.cfo_filter,'{}'::jsonb)
 FROM pl_task t LEFT JOIN pl_task_template tpl ON tpl.id=t.template_id
 WHERE t.pl_id=$1 AND t.form_code=$2 AND t.stage_code='1.1'`, plID, formCode)
	if err != nil {
		return nil, err
	}
	fillers := map[int64]bool{}
	for taskRows.Next() {
		var id int64
		var raw []byte
		if err := taskRows.Scan(&id, &raw); err != nil {
			taskRows.Close()
			return nil, err
		}
		var filter CfoFilter
		if err := json.Unmarshal(raw, &filter); err != nil {
			taskRows.Close()
			return nil, err
		}
		if id > 0 && len(cardsCoveredByTask([]Card{card}, formCode, filter)) > 0 {
			fillers[id] = true
		}
	}
	taskRows.Close()
	if err := taskRows.Err(); err != nil {
		return nil, err
	}
	if len(fillers) == 1 {
		for id := range fillers {
			owners[taskFillStage] = id
		}
	}
	if len(fillers) > 1 {
		return nil, fmt.Errorf("для карточки %s/%s назначено несколько исполнителей заполнения", formCode, scopeKey)
	}
	return owners, nil
}

func ownerDirectory(form string) string {
	if form == TemplateMP {
		return "dir_marketplace"
	}
	return "dir_retail_store"
}

func resolveScopedStepOwners(base map[string]int64, card Card, objects []MpFormPlatform, assignments []UserScope) (map[string]int64, error) {
	out := map[string]int64{}
	for step, id := range base {
		out[step] = id
	}
	grouped := map[string]map[int64][]UserScope{}
	for _, a := range assignments {
		if a.UserID == 0 || a.StageCode == "" {
			continue
		}
		if grouped[a.StageCode] == nil {
			grouped[a.StageCode] = map[int64][]UserScope{}
		}
		grouped[a.StageCode][a.UserID] = append(grouped[a.StageCode][a.UserID], a)
	}
	candidates := map[string]map[int64]bool{}
	for step, users := range grouped {
		candidates[step] = map[int64]bool{}
		for userID, rules := range users {
			// A card approval covers every object. Different complete rules may cover
			// different objects, but a partial platform assignment is not a card owner.
			matches := len(objects) > 0
			for _, p := range objects {
				covered := false
				for _, rule := range rules {
					if ownerRuleAllows(rule, p) {
						covered = true
						break
					}
				}
				if !covered {
					matches = false
					break
				}
			}
			if len(objects) == 0 && card.FormCode != TemplateMP && card.Country != "" {
				for _, rule := range rules {
					if len(rule.CodeCFO) == 0 && ruleAllows(rule, 0, card.Country, card.LegalEntity) {
						matches = true
						break
					}
				}
			}
			if matches {
				candidates[step][userID] = true
			}
		}
		if _, explicitlyScoped := users[out[step]]; explicitlyScoped && !candidates[step][out[step]] {
			delete(out, step)
		}
	}
	for step, ids := range candidates {
		// Existing period assignment is authoritative if it is one of the scoped
		// candidates. Otherwise a unique scoped assignment specifies this card.
		if ids[out[step]] {
			continue
		}
		if len(ids) > 1 {
			return nil, fmt.Errorf("шаг %s карточки %s/%s: неоднозначное назначение ответственного", step, card.FormCode, card.ScopeKey)
		}
		for id := range ids {
			out[step] = id
		}
	}
	return out, nil
}

// An unknown object dimension cannot prove a constrained assignment matches.
func ownerRuleAllows(rule UserScope, p MpFormPlatform) bool {
	if rule.Country != "" && p.Country == "" || rule.LegalEntity != "" && p.LegalEntity == "" {
		return false
	}
	return ruleAllows(rule, p.CodeCFO, p.Country, p.LegalEntity)
}

package plans

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Пресеты представлений формы (миграция 0033).
// ТЗ Розница §4.2/§4.6: состав и порядок колонок, режим группировки
// («статья → магазины» / «магазин → статьи»), уровень детализации настраиваются
// пользователем; последнее использованное представление восстанавливается при
// следующем входе. Имя "" — это и есть «последнее использованное».

// ViewPreset — сохранённое представление формы.
type ViewPreset struct {
	Name      string         `json:"name"`
	IsDefault bool           `json:"is_default"`
	Payload   map[string]any `json:"payload"`
}

// PresetStore — хранилище пресетов.
type PresetStore struct{ pool *pgxpool.Pool }

// NewPresetStore — конструктор.
func NewPresetStore(pool *pgxpool.Pool) *PresetStore { return &PresetStore{pool: pool} }

// Presets — пресеты пользователя по форме.
func (s *PresetStore) Presets(ctx context.Context, userID int64, formCode string) ([]ViewPreset, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name, is_default, payload FROM plans_view_preset
		 WHERE user_id = $1 AND form_code = $2 ORDER BY name`, userID, formCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ViewPreset, 0)
	for rows.Next() {
		var p ViewPreset
		var raw []byte
		if err := rows.Scan(&p.Name, &p.IsDefault, &raw); err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &p.Payload)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Save — создать/обновить пресет (имя "" = последнее использованное представление).
func (s *PresetStore) Save(ctx context.Context, userID int64, formCode string, p ViewPreset) error {
	raw, err := json.Marshal(p.Payload)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO plans_view_preset (user_id, form_code, name, is_default, payload, updated_at)
		VALUES ($1,$2,$3,$4,$5,NOW())
		ON CONFLICT (user_id, form_code, name)
		DO UPDATE SET is_default = EXCLUDED.is_default, payload = EXCLUDED.payload, updated_at = NOW()`,
		userID, formCode, strings.TrimSpace(p.Name), p.IsDefault, raw)
	return err
}

// Delete — удалить именованный пресет.
func (s *PresetStore) Delete(ctx context.Context, userID int64, formCode, name string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM plans_view_preset WHERE user_id = $1 AND form_code = $2 AND name = $3`,
		userID, formCode, name)
	return err
}

// PresetsGet — GET /api/plans/forms/{formCode}/presets.
func (h *CardHandler) PresetsGet(w http.ResponseWriter, r *http.Request) {
	if h.presets == nil {
		writeJSON(w, http.StatusOK, []ViewPreset{})
		return
	}
	p := h.principal(r)
	list, err := h.presets.Presets(r.Context(), p.UserID, r.PathValue("formCode"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// PresetsSave — PUT /api/plans/forms/{formCode}/presets.
func (h *CardHandler) PresetsSave(w http.ResponseWriter, r *http.Request) {
	if h.presets == nil {
		writeErr(w, http.StatusServiceUnavailable, "пресеты недоступны")
		return
	}
	var body ViewPreset
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	p := h.principal(r)
	form := r.PathValue("formCode")
	if err := h.presets.Save(r.Context(), p.UserID, form, body); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	list, _ := h.presets.Presets(r.Context(), p.UserID, form)
	writeJSON(w, http.StatusOK, list)
}

// PresetsDelete — DELETE /api/plans/forms/{formCode}/presets?name=…
func (h *CardHandler) PresetsDelete(w http.ResponseWriter, r *http.Request) {
	if h.presets == nil {
		writeErr(w, http.StatusServiceUnavailable, "пресеты недоступны")
		return
	}
	p := h.principal(r)
	form := r.PathValue("formCode")
	if err := h.presets.Delete(r.Context(), p.UserID, form, r.URL.Query().Get("name")); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	list, _ := h.presets.Presets(r.Context(), p.UserID, form)
	writeJSON(w, http.StatusOK, list)
}

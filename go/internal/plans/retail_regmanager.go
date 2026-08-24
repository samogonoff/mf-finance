package plans

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Справочник соответствия «пользователь ↔ RegManager» (ТЗ Розница §9).
//
// Права РМ определяются полем `[001 CodeCFO].RegManager`, где хранится ФИО, а не
// логин. Автоматическая связка через `FinDWH.dbo.DimEmployee` (§12 п.3) пока
// невозможна — доступ и наличие доменного логина в справочнике не подтверждены.
// Поэтому соответствие ведётся в приложении, а этот файл даёт админу возможность
// его заполнить: без него ни один РМ не увидит своих магазинов, и форма будет
// работать только у финансиста и админа.
//
// `login` нужен для сотрудников, ещё не заходивших в кабинет: строку заводят
// заранее, а `user_id` подставляется при первом входе (сопоставление по логину).

// RegManagerLink — одна строка соответствия.
type RegManagerLink struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	UserName   string `json:"user_name"`
	Login      string `json:"login"`
	RegManager string `json:"reg_manager"`
	Note       string `json:"note"`
	// StoreCount — сколько активных магазинов закреплено за этим РМ в справочнике:
	// сразу видно, что связка ведёт к реальным строкам, а не к опечатке в ФИО.
	StoreCount int `json:"store_count"`
}

// RegManagerStore — CRUD соответствий.
type RegManagerStore struct{ pool *pgxpool.Pool }

// NewRegManagerStore — конструктор.
func NewRegManagerStore(pool *pgxpool.Pool) *RegManagerStore { return &RegManagerStore{pool: pool} }

// Links — все соответствия с количеством магазинов у каждого РМ.
func (s *RegManagerStore) Links(ctx context.Context) ([]RegManagerLink, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, COALESCE(m.user_id, 0), COALESCE(u.name, ''), m.login, m.reg_manager, m.note,
		       (SELECT COUNT(*) FROM plans_directory_row r
		          JOIN plans_directory d ON d.id = r.directory_id
		         WHERE d.code = 'dir_retail_store'
		           AND r.payload_json->>'reg_manager' = m.reg_manager) AS store_count
		  FROM tp_reg_manager_map m
		  LEFT JOIN users u ON u.id = m.user_id
		 ORDER BY m.reg_manager, m.login`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RegManagerLink, 0)
	for rows.Next() {
		var l RegManagerLink
		if err := rows.Scan(&l.ID, &l.UserID, &l.UserName, &l.Login, &l.RegManager,
			&l.Note, &l.StoreCount); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// KnownRegManagers — значения RegManager из справочника магазинов, к которым можно
// привязывать людей. Закрытые точки (Closed / n/a) исключены — ТЗ §9 прямо
// требует их не учитывать.
func (s *RegManagerStore) KnownRegManagers(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT r.payload_json->>'reg_manager' AS rm
		  FROM plans_directory_row r
		  JOIN plans_directory d ON d.id = r.directory_id
		 WHERE d.code = 'dir_retail_store'
		   AND COALESCE(r.payload_json->>'reg_manager', '') <> ''
		 ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var rm string
		if err := rows.Scan(&rm); err != nil {
			return nil, err
		}
		if !retailRegManagerExcluded(rm) {
			out = append(out, rm)
		}
	}
	return out, rows.Err()
}

// Upsert — создать/обновить соответствие.
func (s *RegManagerStore) Upsert(ctx context.Context, l RegManagerLink, actorID int64) error {
	if strings.TrimSpace(l.RegManager) == "" {
		return errors.New("не указан РМ (значение RegManager справочника)")
	}
	if l.UserID == 0 && strings.TrimSpace(l.Login) == "" {
		return errors.New("укажите пользователя кабинета или доменный логин")
	}
	var user, actor any
	if l.UserID != 0 {
		user = l.UserID
	}
	if actorID != 0 {
		actor = actorID
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tp_reg_manager_map (user_id, login, reg_manager, note, updated_by, updated_at)
		VALUES ($1,$2,$3,$4,$5,NOW())
		ON CONFLICT (reg_manager, login) DO UPDATE SET
			user_id = EXCLUDED.user_id, note = EXCLUDED.note,
			updated_by = EXCLUDED.updated_by, updated_at = NOW()`,
		user, strings.TrimSpace(l.Login), strings.TrimSpace(l.RegManager), l.Note, actor)
	return err
}

// Delete — снять соответствие.
func (s *RegManagerStore) Delete(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM tp_reg_manager_map WHERE id = $1`, id)
	return err
}

// ---------- HTTP ----------

// RegManagerHandler — админские ручки соответствий.
type RegManagerHandler struct {
	store *RegManagerStore
	prin  PrincipalFunc
}

// NewRegManagerHandler — конструктор.
func NewRegManagerHandler(store *RegManagerStore, prin PrincipalFunc) *RegManagerHandler {
	return &RegManagerHandler{store: store, prin: prin}
}

// List — GET /api/plans/retail/reg-managers. Соответствия + доступные значения РМ.
func (h *RegManagerHandler) List(w http.ResponseWriter, r *http.Request) {
	links, err := h.store.Links(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	known, err := h.store.KnownRegManagers(r.Context())
	if err != nil {
		// Справочник магазинов может быть ещё не синхронизирован — это не повод
		// прятать уже заведённые соответствия.
		known = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links, "known": known})
}

// Save — PUT /api/plans/retail/reg-managers.
func (h *RegManagerHandler) Save(w http.ResponseWriter, r *http.Request) {
	var body RegManagerLink
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	var actor int64
	if h.prin != nil {
		p, _ := h.prin(r)
		actor = p.UserID
	}
	if err := h.store.Upsert(r.Context(), body, actor); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	links, _ := h.store.Links(r.Context())
	writeJSON(w, http.StatusOK, links)
}

// Delete — DELETE /api/plans/retail/reg-managers/{id}.
func (h *RegManagerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	links, _ := h.store.Links(r.Context())
	writeJSON(w, http.StatusOK, links)
}

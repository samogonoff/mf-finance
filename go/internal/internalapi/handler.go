// Package internalapi — служебные эндпоинты, доступные только другим сервисам
// контура (python-cost и т.п.), а не пользовательскому фронту.
//
// Аутентификация: статический токен в заголовке X-Internal-Token. Значение
// берётся из ENV INTERNAL_SERVICE_TOKEN, в dev задаётся в docker-compose.dev.yml
// и docker-compose.cost.yml одинаковым (фиксированным) значением.
//
// Если ENV INTERNAL_SERVICE_TOKEN пуст — все /internal/* эндпоинты возвращают
// 503, чтобы случайно не оставить открытыми (fail-closed).
package internalapi

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"

	"github.com/company/finance-api/internal/auth"
	"github.com/company/finance-api/internal/notifications"
)

type Handler struct {
	token    string
	users    *auth.UserRepo
	notifSvc *notifications.Service
}

func NewHandler(users *auth.UserRepo, notifSvc *notifications.Service) *Handler {
	return &Handler{
		token:    os.Getenv("INTERNAL_SERVICE_TOKEN"),
		users:    users,
		notifSvc: notifSvc,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// RequireToken — middleware, обязательное для всех /internal/* эндпоинтов.
func (h *Handler) RequireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.token == "" {
			// Fail-closed: токен не сконфигурирован → канал отключён целиком.
			writeErr(w, http.StatusServiceUnavailable, "internal api disabled")
			return
		}
		if r.Header.Get("X-Internal-Token") != h.token {
			writeErr(w, http.StatusUnauthorized, "invalid internal token")
			return
		}
		next.ServeHTTP(w, r)
	}
}

// notifyBody — payload для POST /internal/notifications.
// Указывается либо UserID (адрес конкретному), либо RoleTarget (массово
// носителям роли). Передача обоих — UserID имеет приоритет.
type notifyBody struct {
	UserID     int64          `json:"user_id"`
	RoleTarget string         `json:"role_target"` // например, "ROLE_ADMIN"
	Title      string         `json:"title"`
	Message    string         `json:"message"`
	Type       string         `json:"type"`
	ObjectType string         `json:"object_type"`
	Data       map[string]any `json:"data"`
}

// Notify — POST /internal/notifications
func (h *Handler) Notify(w http.ResponseWriter, r *http.Request) {
	var body notifyBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.Title == "" {
		writeErr(w, http.StatusBadRequest, "title required")
		return
	}
	in := notifications.Input{
		Title:      body.Title,
		Message:    body.Message,
		Type:       body.Type,
		ObjectType: body.ObjectType,
		Data:       body.Data,
	}

	if body.UserID > 0 {
		in.UserID = body.UserID
		n, err := h.notifSvc.Create(r.Context(), in)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"created": 1, "id": n.ID})
		return
	}

	if body.RoleTarget == "" {
		writeErr(w, http.StatusBadRequest, "user_id or role_target required")
		return
	}
	if body.RoleTarget != auth.RoleAdmin {
		// На старте — массовое можно делать только для админов; точечную
		// адресацию по другим ролям делаем через user_id, чтобы не открывать
		// аналог spray-эндпоинта по всем юзерам.
		writeErr(w, http.StatusBadRequest, "role_target supported only for ROLE_ADMIN")
		return
	}
	n, err := h.notifSvc.CreateForAllAdmins(r.Context(), in)
	if err != nil {
		log.Printf("internalapi: CreateForAllAdmins: %v", err)
		// возвращаем 200, если хотя бы что-то создалось — иначе 500.
		if n == 0 {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]int{"created": n})
}

// Ensure mode: на старте если токена нет — пишем предупреждение в лог.
// Вызывается main.go после создания хендлера.
func (h *Handler) Verify(ctx context.Context) error {
	if h.token == "" {
		log.Printf("internalapi: INTERNAL_SERVICE_TOKEN не задан — /internal/* выключены")
		return errors.New("token not set")
	}
	return nil
}

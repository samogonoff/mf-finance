package plans

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Импорт пользователей из Bitrix24 по ID (аналог MP: админ догружает сотрудников,
// даже не заходивших). Источник — inbound-вебхук B24 с правом user.get
// (B24_USERGET_WEBHOOK), серверный резолв без токена админа. Догруженные юзеры
// потом назначаются директорами/ТОПами ЮЛ и ответственными по должностям.

// B24Importer резолвит профили из B24 и upsert'ит в users.
type B24Importer struct {
	webhook string
	users   *UsersStore
	http    *http.Client
}

// NewB24Importer — конструктор. Пустой webhook → Import вернёт ошибку.
func NewB24Importer(webhook string, users *UsersStore) *B24Importer {
	return &B24Importer{
		webhook: strings.TrimRight(webhook, "/") + "/",
		users:   users,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// Enabled — задан ли вебхук.
func (b *B24Importer) Enabled() bool { return b.webhook != "/" }

// ImportResult — итог по одному B24-id.
type ImportResult struct {
	B24ID  int64  `json:"b24_id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Status string `json:"status"` // created|updated|not_found|error
	Error  string `json:"error,omitempty"`
}

type b24UserGetResp struct {
	Result []struct {
		ID       string `json:"ID"`
		Name     string `json:"NAME"`
		LastName string `json:"LAST_NAME"`
		Email    string `json:"EMAIL"`
		Login    string `json:"LOGIN"`
	} `json:"result"`
	Error     string `json:"error"`
	ErrorDesc string `json:"error_description"`
}

// domainFromWebhook вытаскивает домен B24 из URL вебхука (для users.b24_domain).
func (b *B24Importer) domain() string {
	if u, err := url.Parse(b.webhook); err == nil {
		return u.Host
	}
	return ""
}

// Import резолвит и upsert'ит пользователей по списку B24-id.
func (b *B24Importer) Import(ctx context.Context, ids []int64) ([]ImportResult, error) {
	if !b.Enabled() {
		return nil, fmt.Errorf("B24_USERGET_WEBHOOK не задан")
	}
	dom := b.domain()
	out := make([]ImportResult, 0, len(ids))
	for _, id := range ids {
		res := ImportResult{B24ID: id}
		prof, err := b.fetch(ctx, id)
		if err != nil {
			res.Status, res.Error = "error", err.Error()
			out = append(out, res)
			continue
		}
		if prof == nil {
			res.Status = "not_found"
			out = append(out, res)
			continue
		}
		email := prof.Email
		if email == "" {
			email = prof.Login
		}
		res.Name = strings.TrimSpace(prof.Name + " " + prof.LastName)
		res.Email = email
		_, status, err := b.users.UpsertByB24(ctx, id, prof.Name, prof.LastName, email, dom)
		if err != nil {
			res.Status, res.Error = "error", err.Error()
		} else {
			res.Status = status
		}
		out = append(out, res)
	}
	return out, nil
}

func (b *B24Importer) fetch(ctx context.Context, id int64) (*struct {
	Name, LastName, Email, Login string
}, error) {
	u := b.webhook + "user.get.json?ID=" + strconv.FormatInt(id, 10)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := b.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data b24UserGetResp
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	if data.Error != "" {
		return nil, fmt.Errorf("b24: %s", data.Error)
	}
	if len(data.Result) == 0 {
		return nil, nil
	}
	r := data.Result[0]
	return &struct {
		Name, LastName, Email, Login string
	}{r.Name, r.LastName, r.Email, r.Login}, nil
}

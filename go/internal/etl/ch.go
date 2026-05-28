package etl

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// chClient — общий HTTP-клиент для CH-операций.
type chClient struct {
	url    string // http://user:pass@host:port — с креденшалами
	client *http.Client
}

func newCHClient(baseURL, user, password string) (*chClient, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("clickhouse: empty URL")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: parse URL: %w", err)
	}
	u.User = url.UserPassword(user, password)
	return &chClient{
		url:    u.String(),
		client: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// exec выполняет произвольный SQL без обработки результата.
func (c *chClient) exec(ctx context.Context, query string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/?query="+url.QueryEscape(query), nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ch exec status %s: %s", resp.Status, string(body))
	}
	return nil
}

// insertJSON отправляет батч JSONEachRow в указанную таблицу.
func (c *chClient) insertJSON(ctx context.Context, table string, body io.Reader) error {
	q := "INSERT INTO " + table + " FORMAT JSONEachRow"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/?query="+url.QueryEscape(q), body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		rb, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ch insert %s status %s: %s", table, resp.Status, string(rb))
	}
	return nil
}

// queryString возвращает результат как строку (для одиночных SELECT'ов).
func (c *chClient) queryString(ctx context.Context, query string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/", strings.NewReader(query))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("ch query status %s: %s", resp.Status, string(body))
	}
	return strings.TrimSpace(string(body)), nil
}

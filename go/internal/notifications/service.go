package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/company/finance-api/internal/auth"
)

// MessagePrefix — декорирует сообщение в site_api (как «ЛК МП: » в MP).
// Помогает оперативно отличать кабинет-источник в Битриксе.
const MessagePrefix = "Finance: "

// Service — фасад над репозиторием + ассинхронная доставка в Б24
// через site_api (кастомный mfportal-прокси, шлёт в im.notify).
//
// Эталон: MP src/Service/Site/SiteApiNotifyService.php +
// MessageHandler/SendBitrix24NotificationMessageHandler.php.
type Service struct {
	repo  *Repo
	users *auth.UserRepo

	// site_api конфиг. Все три должны быть заданы → дублирование включено.
	siteURL  string
	siteUser string
	sitePass string

	httpC *http.Client
}

func NewService(repo *Repo, users *auth.UserRepo) *Service {
	return &Service{
		repo:     repo,
		users:    users,
		siteURL:  os.Getenv("SITE_API_NOTIFY_URL"),
		siteUser: os.Getenv("SITE_API_NOTIFY_USER"),
		sitePass: os.Getenv("SITE_API_NOTIFY_PASSWORD"),
		httpC:    &http.Client{Timeout: 8 * time.Second},
	}
}

// B24Enabled — true, если все три ENV заданы. Это автоматически делает
// дублирование «prod-only»: в dev .env.example значения пустые.
func (s *Service) B24Enabled() bool {
	return s.siteURL != "" && s.siteUser != "" && s.sitePass != ""
}

// Create — основной вход. Создаёт уведомление и (если применимо) запускает
// fire-and-forget доставку в Б24. Возврат до завершения доставки.
func (s *Service) Create(ctx context.Context, in Input) (*Notification, error) {
	n, err := s.repo.Create(ctx, in)
	if err != nil {
		return nil, err
	}
	if s.B24Enabled() {
		u, err := s.users.ByID(ctx, in.UserID)
		if err == nil && u.NotifyViaB24 && u.B24ID != nil {
			go s.deliverToSiteAPI(*u.B24ID, n)
		}
	}
	return n, nil
}

// CreateForAllAdmins — массово создаёт уведомления для всех носителей ROLE_ADMIN
// (с учётом иерархии — фактически только direct ROLE_ADMIN, т.к. иерархия
// разворачивается «вниз», а ROLE_ADMIN — корень). Возвращает число созданных.
func (s *Service) CreateForAllAdmins(ctx context.Context, in Input) (int, error) {
	blocked := false
	page, _, err := s.users.List(ctx, auth.ListFilter{IsBlocked: &blocked, Limit: 200})
	if err != nil {
		return 0, err
	}
	cnt := 0
	for i := range page {
		u := &page[i]
		if !auth.HasRole(u, auth.RoleAdmin) {
			continue
		}
		in.UserID = u.ID
		if _, err := s.Create(ctx, in); err != nil {
			log.Printf("notifications: admin notify failed for user %d: %v", u.ID, err)
			continue
		}
		cnt++
	}
	return cnt, nil
}

// CreateWelcome — приветственное уведомление при первом успешном логине.
// Идемпотентно по флагу users.welcome_notification_sent.
func (s *Service) CreateWelcome(ctx context.Context, u *auth.User) error {
	if u == nil || u.WelcomeNotificationSent {
		return nil
	}
	_, err := s.Create(ctx, Input{
		UserID:     u.ID,
		Title:      "Добро пожаловать в Finance Cabinet",
		Message:    "Если что-то выглядит не так — используйте кнопку «Сообщить о проблеме» внизу страницы.",
		Type:       TypeInfo,
		ObjectType: ObjWelcome,
	})
	if err != nil {
		return err
	}
	return s.users.SetWelcomeSent(ctx, u.ID)
}

// deliverToSiteAPI — POST JSON {id, message} с Basic Auth на site_api.
// id — это b24Id пользователя (число как строка), как у MP.
// Ошибки складываем в аудит-поля notifications (b24_attempts, b24_last_error);
// retry-цикла на старте нет.
func (s *Service) deliverToSiteAPI(b24UserID int64, n *Notification) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	text := n.Title
	if n.Message != "" {
		text += "\n" + n.Message
	}
	body, _ := json.Marshal(map[string]string{
		"id":      fmt.Sprintf("%d", b24UserID),
		"message": MessagePrefix + text,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.siteURL, bytes.NewReader(body))
	if err != nil {
		_ = s.repo.UpdateB24Status(ctx, n.ID, false, "build req: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(s.siteUser, s.sitePass)

	resp, err := s.httpC.Do(req)
	if err != nil {
		_ = s.repo.UpdateB24Status(ctx, n.ID, false, err.Error())
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_ = s.repo.UpdateB24Status(ctx, n.ID, false, fmt.Sprintf("HTTP %d", resp.StatusCode))
		return
	}

	// site_api возвращает {"success": bool, ...}. Считаем неуспешным, если success != true.
	var rb struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&rb)
	if !rb.Success {
		msg := rb.Error
		if msg == "" {
			msg = "site_api returned success=false"
		}
		_ = s.repo.UpdateB24Status(ctx, n.ID, false, msg)
		return
	}
	_ = s.repo.UpdateB24Status(ctx, n.ID, true, "")
}

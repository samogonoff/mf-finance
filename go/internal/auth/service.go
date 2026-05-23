package auth

import (
	"context"
	"errors"
	"strconv"
)

// B24CallbackPayload — что пришло от Nuxt-сервера после OAuth.
type B24CallbackPayload struct {
	Email     string  `json:"email"`
	Name      string  `json:"name"`
	LastName  string  `json:"lastName"`
	B24ID     *int64  `json:"-"`
	B24IDRaw  any     `json:"b24Id"`
	B24Domain string  `json:"b24Domain"`
	Photo     *string `json:"photo,omitempty"`
	Position  *string `json:"position,omitempty"`
}

func (p *B24CallbackPayload) Normalize() {
	switch v := p.B24IDRaw.(type) {
	case string:
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			p.B24ID = &id
		}
	case float64:
		id := int64(v)
		p.B24ID = &id
	case int64:
		id := v
		p.B24ID = &id
	}
}

// PostLoginHook вызывается из HandleB24Callback после успешной выдачи токена.
// Передан как extension-point, чтобы избежать цикла зависимостей с
// пакетом notifications (он импортирует auth). Если nil — не вызывается.
type PostLoginHook func(ctx context.Context, u *User)

type Service struct {
	users    *UserRepo
	tokens   *TokenRepo
	onLogin  PostLoginHook
}

func NewService(u *UserRepo, t *TokenRepo) *Service {
	return &Service{users: u, tokens: t}
}

// SetPostLoginHook регистрирует пост-логин-обработчик (welcome-уведомление и т.п.).
func (s *Service) SetPostLoginHook(h PostLoginHook) { s.onLogin = h }

type IssuedSession struct {
	Access      string
	Refresh     string
	ExpiresIn   int64
	User        *User
}

func (s *Service) HandleB24Callback(ctx context.Context, in B24CallbackPayload) (*IssuedSession, error) {
	if in.Email == "" {
		return nil, errors.New("email required")
	}
	in.Normalize()
	u, err := s.users.FindOrCreateByB24(ctx, in)
	if err != nil {
		return nil, err
	}
	access, refresh, err := s.tokens.Issue(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	if s.onLogin != nil {
		s.onLogin(ctx, u)
	}
	return &IssuedSession{
		Access: access, Refresh: refresh, ExpiresIn: int64(accessTTL.Seconds()), User: u,
	}, nil
}

func (s *Service) RefreshSession(ctx context.Context, refresh string) (*IssuedSession, error) {
	uid, err := s.tokens.RefreshUser(ctx, refresh)
	if err != nil {
		return nil, err
	}
	if uid == 0 {
		return nil, errors.New("invalid refresh token")
	}
	access, err := s.tokens.IssueAccess(ctx, uid)
	if err != nil {
		return nil, err
	}
	u, err := s.users.ByID(ctx, uid)
	if err != nil {
		return nil, err
	}
	return &IssuedSession{
		Access: access, Refresh: refresh, ExpiresIn: int64(accessTTL.Seconds()), User: u,
	}, nil
}

// AuthenticateBearer — проверка access-токена middleware'ом.
func (s *Service) AuthenticateBearer(ctx context.Context, access string) (*User, error) {
	uid, err := s.tokens.AccessUser(ctx, access)
	if err != nil {
		return nil, err
	}
	if uid == 0 {
		return nil, errors.New("invalid token")
	}
	return s.users.ByID(ctx, uid)
}

func (s *Service) Logout(ctx context.Context, access string) error {
	return s.tokens.Revoke(ctx, access)
}

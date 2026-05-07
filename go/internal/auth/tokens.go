package auth

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	accessTTL  = 1 * time.Hour
	refreshTTL = 30 * 24 * time.Hour

	prefixAccess  = "auth_token:"
	prefixRefresh = "refresh_token:"
	prefixUserSet = "user_tokens:"
)

// TokenRepo — обёртка над Redis для хранения access/refresh токенов.
// Схема ключей и TTL копируется из MP (см. FINANCE_PORTING_GUIDE 1.5).
type TokenRepo struct{ rdb *redis.Client }

func NewTokenRepo(rdb *redis.Client) *TokenRepo { return &TokenRepo{rdb: rdb} }

// Issue — выпускает пару (access, refresh) для userID.
func (t *TokenRepo) Issue(ctx context.Context, userID int64) (access, refresh string, err error) {
	access = uuid.NewString()
	refresh = uuid.NewString()

	uid := strconv.FormatInt(userID, 10)

	pipe := t.rdb.TxPipeline()
	pipe.Set(ctx, prefixAccess+access, uid, accessTTL)
	pipe.Set(ctx, prefixRefresh+refresh, uid, refreshTTL)
	pipe.SAdd(ctx, prefixUserSet+uid, access, refresh)
	if _, err = pipe.Exec(ctx); err != nil {
		return "", "", err
	}
	return access, refresh, nil
}

// AccessUser — возвращает userID по access-токену; 0 + nil если нет/expired.
func (t *TokenRepo) AccessUser(ctx context.Context, access string) (int64, error) {
	v, err := t.rdb.Get(ctx, prefixAccess+access).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

// RefreshUser — userID по refresh-токену.
func (t *TokenRepo) RefreshUser(ctx context.Context, refresh string) (int64, error) {
	v, err := t.rdb.Get(ctx, prefixRefresh+refresh).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

// IssueAccess — выдаёт новый access без перевыпуска refresh.
func (t *TokenRepo) IssueAccess(ctx context.Context, userID int64) (string, error) {
	access := uuid.NewString()
	uid := strconv.FormatInt(userID, 10)
	pipe := t.rdb.TxPipeline()
	pipe.Set(ctx, prefixAccess+access, uid, accessTTL)
	pipe.SAdd(ctx, prefixUserSet+uid, access)
	_, err := pipe.Exec(ctx)
	return access, err
}

// Revoke — удаляет конкретный access-токен; refresh не трогаем.
func (t *TokenRepo) Revoke(ctx context.Context, access string) error {
	return t.rdb.Del(ctx, prefixAccess+access).Err()
}

// RevokeAll — удаляет все токены пользователя (для logout-all).
func (t *TokenRepo) RevokeAll(ctx context.Context, userID int64) error {
	uid := strconv.FormatInt(userID, 10)
	tokens, err := t.rdb.SMembers(ctx, prefixUserSet+uid).Result()
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(tokens)*2+1)
	for _, tok := range tokens {
		keys = append(keys, prefixAccess+tok, prefixRefresh+tok)
	}
	keys = append(keys, prefixUserSet+uid)
	if len(keys) == 0 {
		return nil
	}
	return t.rdb.Del(ctx, keys...).Err()
}

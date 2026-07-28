package gatewayticket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

var ErrNotFound = errors.New("gateway ticket not found")

type Ticket struct {
	UserID               int64 `json:"user_id"`
	SessionID            int64 `json:"session_id"`
	AccessTokenExpiresAt int64 `json:"access_token_expires_at"`
}

type Store interface {
	Put(ctx context.Context, tokenHash string, ticket Ticket, ttl time.Duration) error
	Redeem(ctx context.Context, tokenHash string) (Ticket, error)
}

type RedisStore struct {
	rds       *redis.Redis
	keyPrefix string
}

func NewRedisStore(rds *redis.Redis, keyPrefix string) Store {
	return &RedisStore{rds: rds, keyPrefix: keyPrefix}
}

func (s *RedisStore) Put(ctx context.Context, tokenHash string, ticket Ticket, ttl time.Duration) error {
	value, err := json.Marshal(ticket)
	if err != nil {
		return fmt.Errorf("marshal gateway ticket: %w", err)
	}
	if err := s.rds.SetexCtx(ctx, s.keyPrefix+tokenHash, string(value), max(int(ttl/time.Second), 1)); err != nil {
		return fmt.Errorf("store gateway ticket: %w", err)
	}
	return nil
}

func (s *RedisStore) Redeem(ctx context.Context, tokenHash string) (Ticket, error) {
	value, err := s.rds.GetDelCtx(ctx, s.keyPrefix+tokenHash)
	if errors.Is(err, redis.Nil) {
		return Ticket{}, ErrNotFound
	}
	if err != nil {
		return Ticket{}, fmt.Errorf("redeem gateway ticket: %w", err)
	}
	if value == "" {
		return Ticket{}, ErrNotFound
	}
	var ticket Ticket
	if err := json.Unmarshal([]byte(value), &ticket); err != nil {
		return Ticket{}, fmt.Errorf("unmarshal gateway ticket: %w", err)
	}
	return ticket, nil
}

package common

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

const (
	ContextKeyUserID    contextKey = "user_id"
	ContextKeyRequestID contextKey = "request_id"
	ContextKeyJTI       contextKey = "jti"
	ContextKeyExpTime   contextKey = "exp"
)

const RedisKeyBlocklist = "blocklist:"

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	val := ctx.Value(ContextKeyUserID)
	if val == nil {
		return uuid.Nil, false
	}
	id, ok := val.(uuid.UUID)
	return id, ok
}

func WithUserID(ctx context.Context, userID uuid.UUID) context.Context {
	return context.WithValue(ctx, ContextKeyUserID, userID)
}

func RequestIDFromContext(ctx context.Context) string {
	val := ctx.Value(ContextKeyRequestID)
	if val == nil {
		return ""
	}
	return val.(string)
}

func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, ContextKeyRequestID, requestID)
}

func RequestExpFromContext(ctx context.Context) int64 {
	val := ctx.Value(ContextKeyExpTime)
	if val == nil {
		return 1
	}
	return val.(int64)
}

// common/context.go
package common

import (
	"context"

	"github.com/google/uuid"
)

type contextKey string

const (
	ContextKeyUserID    contextKey = "user_id"
	ContextKeyRequestID contextKey = "request_id"
)

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

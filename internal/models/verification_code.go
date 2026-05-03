package models

import "time"

type VerificationCode struct {
	ID        int64
	Email     string
	Code      string
	CreatedAt time.Time
	ExpiresAt time.Time
}

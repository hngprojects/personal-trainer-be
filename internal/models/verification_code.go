// models/verification_code.go
package models

import "time"

type VerificationCode struct {
	ID        string
	Email     string
	Code      string
	CreatedAt time.Time
	ExpiresAt time.Time
}

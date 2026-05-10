package models

import (
	"time"

	"github.com/google/uuid"
)

type AuthProvider string

const (
	AuthProviderLocal  AuthProvider = "local"
	AuthProviderGoogle AuthProvider = "google"
)

type User struct {
	ID           string
	Email        string
	Name         string
	Password     *string
	AuthProvider AuthProvider
	IsActive     bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Role struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

type UserRole struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	RoleID    uuid.UUID
	CreatedAt time.Time
}

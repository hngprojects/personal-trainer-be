// models/user.go
package models

import "time"

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

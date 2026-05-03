package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hngprojects/personal-trainer-be/internal/service"
)

type AuthHandler struct {
	auth *service.AuthService
}

func NewAuthHandler(auth *service.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

// POST /auth/register
func (h *AuthHandler) InitiateSignUp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "valid email is required")
		return
	}

	if err := h.auth.InitiateSignUp(r.Context(), req.Email); err != nil {
		if errors.Is(err, service.ErrEmailAlreadyExists) {
			writeError(w, http.StatusConflict, "EMAIL_EXISTS", "email already registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "verification code sent"})
}

// POST /auth/register/verify
func (h *AuthHandler) VerifyCode(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		Code  string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Code == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "email and code are required")
		return
	}

	if err := h.auth.VerifyCode(r.Context(), req.Email, req.Code); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_CODE", "invalid or expired verification code")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "code verified"})
}

// POST /auth/register/complete
func (h *AuthHandler) CompleteSignUp(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil ||
		req.Email == "" || req.Name == "" || req.Code == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "email, name, code, and password are required")
		return
	}

	session, err := h.auth.CompleteSignUp(r.Context(), req.Email, req.Name, req.Code, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCode):
			writeError(w, http.StatusBadRequest, "INVALID_CODE", "invalid or expired verification code")
		case errors.Is(err, service.ErrWeakPassword):
			writeError(w, http.StatusBadRequest, "WEAK_PASSWORD", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"status": "account created",
		"data": map[string]any{
			"session_id": session.ID,
			"expires_at": session.ExpiresAt,
		},
	})
}

// POST /auth/login
func (h *AuthHandler) SignIn(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "VALIDATION_FAILED", "email and password are required")
		return
	}

	session, user, err := h.auth.SignIn(r.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid email or password")
		case errors.Is(err, service.ErrAccountNotActive):
			writeError(w, http.StatusForbidden, "ACCOUNT_INACTIVE", "account is not active")
		default:
			writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "something went wrong")
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "logged in",
		"data": map[string]any{
			"session_id": session.ID,
			"expires_at": session.ExpiresAt,
			"user": map[string]any{
				"id":    user.ID,
				"email": user.Email,
				"name":  user.Name,
			},
		},
	})
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
}

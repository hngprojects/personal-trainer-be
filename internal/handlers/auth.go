package handlers

import (
	"net/http"
	"strings"

	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	"github.com/hngprojects/personal-trainer-be/internal/service"
)

type LocalAuthHandler struct {
	authService *service.AuthService
}

func NewLocalAuthHandler(authService *service.AuthService) *LocalAuthHandler {
	return &LocalAuthHandler{authService: authService}
}

func (h *LocalAuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	token := strings.SplitN(r.Header.Get("Authorization"), " ", 2)[1]

	if err := h.authService.Logout(r.Context(), token); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": map[string]any{
				"code":    "LOGOUT_FAILED",
				"message": "could not log out",
			},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   map[string]any{"message": "logged out successfully"},
	})
}

func (h *LocalAuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := r.Context().Value(middleware.UserIDKey).(int64)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": map[string]any{
				"code":    "UNAUTHORIZED",
				"message": "unauthorized",
			},
		})
		return
	}

	var input struct {
		NewPassword string `json:"new_password"`
	}
	if err := readJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{
				"code":    "INVALID_INPUT",
				"message": "invalid request body",
			},
		})
		return
	}

	if err := h.authService.ChangePassword(r.Context(), userID, input.NewPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": map[string]any{
				"code":    "VALIDATION_FAILED",
				"message": err.Error(),
			},
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   map[string]any{"message": "password updated successfully"},
	})
}
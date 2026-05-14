package dev

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/config"
)

type Handler struct {
	log *slog.Logger
}

func NewDevHandler() *Handler {
	return &Handler{}
}

var cfg *config.Config

func (h *Handler) HandleCreateDevToken(c *gin.Context) {
	type response struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	generatedToken, err := auth.GenerateJWTToken(uuid.New().String(), "access")
	if err != nil {
		h.log.Error("failed to generate token", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("Internal server error", api.CodeServerError))
		return
	}

	data := response{
		AccessToken: generatedToken,
	}
	c.JSON(http.StatusOK, api.NewSuccess("Success", api.CodeOK, data))

}

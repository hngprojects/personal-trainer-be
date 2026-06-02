package dev

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
)

type Handler struct {
	log *slog.Logger
}

func NewDevHandler() *Handler {
	return &Handler{}
}

func (h *Handler) HandleCreateDevToken(c *gin.Context, params api.HandleCreateDevTokenParams) {
	type response struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	var userID string
	if params.UserId != nil {
		userID = *params.UserId
	}
	if params.UserId == nil {
		userID = uuid.New().String()
	} else if _, err := uuid.Parse(userID); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("user_id must be a valid UUID", api.CodeBadRequest))
		return
	}

	accessToken, refreshToken, err := auth.GenerateTestTokens(userID)
	if err != nil {
		h.log.Error("failed to generate tokens", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("Internal server error", api.CodeServerError))
		return
	}

	data := response{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}
	c.JSON(http.StatusOK, api.NewSuccess("Success", api.CodeOK, data))
}

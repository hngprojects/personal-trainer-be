package auth

import (
	"net/http"
	"time"

	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
)

type LogoutHandler struct {
	redis appredis.RedisClient
	log   *slog.Logger
}

func NewLogoutHandler(redis appredis.RedisClient, log *slog.Logger) *LogoutHandler {
	return &LogoutHandler{redis: redis, log: log}
}

func (h *LogoutHandler) HandleLogout(c *gin.Context) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}

	if err := c.ShouldBindJSON(&body); err != nil || body.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, api.NewError("refresh token is required", api.CodeBadRequest))
		return
	}

	token, err := ValidateToken(body.RefreshToken)
	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid refresh token", api.CodeUnauthorized))
		return
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid token claims", api.CodeUnauthorized))
		return
	}

	tokenType, _ := claims["type"].(string)
	if tokenType != string(RefreshToken) {
		c.JSON(http.StatusBadRequest, api.NewError("invalid token type", api.CodeBadRequest))
		return
	}

	jti, _ := claims["jti"].(string)
	if jti == "" {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid token", api.CodeUnauthorized))
		return
	}

	if err := h.redis.Set(c.Request.Context(), "blocklist:"+jti, 1, 7*24*time.Hour); err != nil {
		h.log.Error("failed to blocklist token", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccessResponse("logout successful", api.CodeOK, nil, nil))
}

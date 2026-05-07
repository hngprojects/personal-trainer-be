package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/hngprojects/personal-trainer-be/internal/config"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	authsvc "github.com/hngprojects/personal-trainer-be/internal/service"
)

type googleUserInfo struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

type GoogleHandler struct {
	oauthCfg *oauth2.Config
	users    UserRepository
	queries  *db.Queries
	log      *slog.Logger
}

func NewGoogleHandler(cfg *config.Config, users UserRepository, queries *db.Queries, log *slog.Logger) *GoogleHandler {
	return &GoogleHandler{
		users:   users,
		queries: queries,
		log:     log,
		oauthCfg: &oauth2.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			RedirectURL:  cfg.GoogleRedirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		},
	}
}

// GET /auth/google — generates state, sets cookie, redirects to Google.
func (h *GoogleHandler) HandleGoogleLogin(c *gin.Context) {
	state := generateState()
	c.SetCookie("oauth_state", state, 300, "/", "", false, true)
	url := h.oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOnline)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

// GET /auth/google/callback — exchanges code, upserts user, returns JWTs.
func (h *GoogleHandler) HandleGoogleCallback(c *gin.Context) {
	stateFromGoogle := c.Query("state")
	stateFromCookie, err := c.Cookie("oauth_state")
	if err != nil || stateFromGoogle != stateFromCookie {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}

	code := c.Query("code")
	googleToken, err := h.oauthCfg.Exchange(c.Request.Context(), code)
	if err != nil {
		h.log.Error("failed to exchange Google code", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to exchange code"})
		return
	}

	userInfo, err := fetchGoogleUserInfo(c.Request.Context(), h.oauthCfg, googleToken)
	if err != nil {
		h.log.Error("failed to fetch Google user info", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch user info"})
		return
	}

	isNewUser := false
	user, err := h.users.FindByEmailAndProvider(c.Request.Context(), userInfo.Email, "google")
	if err != nil {
		if err != ErrNotFound {
			h.log.Error("database error looking up user", "err", err, "email", userInfo.Email)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
			return
		}
		user, err = h.users.Create(c.Request.Context(), userInfo.Email, userInfo.Name, "google")
		if err != nil {
			h.log.Error("failed to create user", "err", err, "email", userInfo.Email)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
			return
		}
		isNewUser = true
	}

	userIDStr := user.ID.String()
	accessToken, err := authsvc.GenerateJWTToken(userIDStr, authsvc.AccessToken)
	if err != nil {
		h.log.Error("failed to generate access token", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate access token"})
		return
	}
	refreshToken, err := authsvc.GenerateJWTToken(userIDStr, authsvc.RefreshToken)
	if err != nil {
		h.log.Error("failed to generate refresh token", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate refresh token"})
		return
	}

	h.log.Info("google oauth successful", "email", userInfo.Email, "is_new_user", isNewUser)

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"message": "GOOGLE_AUTH_SUCCESSFUL",
		"data": gin.H{
			"user": gin.H{
				"id":               user.ID,
				"email":            user.Email,
				"name":             user.Name,
				"user_type":        "client",
				"profile_complete": false,
			},
			"access_token":  accessToken,
			"refresh_token": refreshToken,
			"is_new_user":   isNewUser,
			"expires_in":    int(10 * time.Minute / time.Second),
		},
	})
}

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func fetchGoogleUserInfo(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token) (*googleUserInfo, error) {
	client := cfg.Client(ctx, token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return nil, fmt.Errorf("fetch userinfo: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read userinfo body: %w", err)
	}

	var info googleUserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	return &info, nil
}

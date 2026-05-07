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
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type googleUserInfo struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

type GoogleHandler struct {
	oauthCfg *oauth2.Config
	users    UserRepository
	log      *slog.Logger
	isProd   bool
}

func NewGoogleHandler(cfg *config.Config, users UserRepository, log *slog.Logger) *GoogleHandler {
	return &GoogleHandler{
		users:  users,
		log:    log,
		isProd: cfg.Env == "production",
		oauthCfg: &oauth2.Config{
			ClientID:     cfg.GoogleClientID,
			ClientSecret: cfg.GoogleClientSecret,
			RedirectURL:  cfg.GoogleRedirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		},
	}
}

func (h *GoogleHandler) HandleGoogleLogin(c *gin.Context) {
	state, err := generateState()
	if err != nil {
		h.log.Error("failed to generate state", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("oauth_state", state, 300, "/", "", h.isProd, true)
	url := h.oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOnline)
	c.Redirect(http.StatusFound, url)
}

func (h *GoogleHandler) HandleGoogleCallback(c *gin.Context, state, code string) {
	stateFromCookie, err := c.Cookie("oauth_state")
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie("oauth_state", "", -1, "/", "", h.isProd, true)

	if err != nil || state != stateFromCookie {
		c.JSON(http.StatusBadRequest, api.NewErrorResponse("invalid state", api.CodeBadRequest, nil))
		return
	}

	googleToken, err := h.oauthCfg.Exchange(c.Request.Context(), code)
	if err != nil {
		h.log.Error("failed to exchange Google code", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to exchange code", api.CodeServerError))
		return
	}

	userInfo, err := fetchGoogleUserInfo(c.Request.Context(), h.oauthCfg, googleToken)
	if err != nil {
		h.log.Error("failed to fetch Google user info", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to fetch user info", api.CodeServerError))
		return
	}

	isNewUser := false
	user, err := h.users.FindByEmailAndProvider(c.Request.Context(), userInfo.Email, "google")
	if err != nil {
		if err != ErrNotFound {
			h.log.Error("database error looking up user", "err", err, "email", userInfo.Email)
			c.JSON(http.StatusInternalServerError, api.NewError("database error", api.CodeServerError))
			return
		}
		user, err = h.users.Create(c.Request.Context(), userInfo.Email, userInfo.Name, "google")
		if err != nil {
			h.log.Error("failed to create user", "err", err, "email", userInfo.Email)
			c.JSON(http.StatusInternalServerError, api.NewError("failed to create user", api.CodeServerError))
			return
		}
		isNewUser = true
	}

	userIDStr := user.ID.String()
	accessToken, err := GenerateJWTToken(userIDStr, AccessToken)
	if err != nil {
		h.log.Error("failed to generate access token", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to generate access token", api.CodeServerError))
		return
	}
	refreshToken, err := GenerateJWTToken(userIDStr, RefreshToken)
	if err != nil {
		h.log.Error("failed to generate refresh token", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to generate refresh token", api.CodeServerError))
		return
	}

	h.log.Info("google oauth successful", "email", userInfo.Email, "is_new_user", isNewUser)

	googleData := map[string]interface{}{
		"user": map[string]interface{}{
			"id":               user.ID.String(),
			"email":            user.Email,
			"name":             user.Name,
			"user_type":        "client",
			"profile_complete": isNewUser,
		},
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"is_new_user":   isNewUser,
		"expires_in":    int(10 * time.Minute / time.Second),
	}
	c.JSON(http.StatusOK, api.NewSuccessResponse("Google authentication successful", api.CodeOK, googleData, nil))
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

func fetchGoogleUserInfo(ctx context.Context, cfg *oauth2.Config, token *oauth2.Token) (*googleUserInfo, error) {
	client := cfg.Client(ctx, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return nil, fmt.Errorf("create userinfo request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch userinfo: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("userinfo request failed with status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read userinfo body: %w", err)
	}

	var info googleUserInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	if info.Email == "" {
		return nil, fmt.Errorf("google userinfo returned empty email")
	}
	return &info, nil
}

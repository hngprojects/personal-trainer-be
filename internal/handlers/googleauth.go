package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/db"
	"github.com/hngprojects/personal-trainer-be/internal/service"
	"golang.org/x/oauth2"
)

func (h *AuthHandler) GoogleLogin(c *gin.Context) {
	state := service.GenerateState()
	c.SetCookie("oauth_state", state, 300, "/", "", false, true)
	url := h.oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOnline)
	c.Redirect(http.StatusTemporaryRedirect, url)
}

func (h *AuthHandler) GoogleCallback(c *gin.Context) {
	stateFromGoogle := c.Query("state")
	stateFromCookie, err := c.Cookie("oauth_state")
	if err != nil || stateFromGoogle != stateFromCookie {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid state"})
		return
	}

	code := c.Query("code")
	googleToken, err := h.oauthCfg.Exchange(c.Request.Context(), code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to exchange code"})
		return
	}

	userInfo, err := service.FetchGoogleUserInfo(c.Request.Context(), h.oauthCfg, googleToken)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get user info"})
		return
	}

	user, err := h.queries.GetUserByEmailAndProvider(c.Request.Context(), db.GetUserByEmailAndProviderParams{
		Email:        userInfo.Email,
		AuthProvider: "google",
	})
	if err != nil {
		if err == sql.ErrNoRows {
			user, err = h.queries.CreateUser(c.Request.Context(), db.CreateUserParams{
				Email:        userInfo.Email,
				Name:         userInfo.Name,
				AuthProvider: "google",
			})
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
				return
			}
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
			return
		}
	}

	sessionToken := service.GenerateSessionToken()
	_, err = h.queries.CreateSession(c.Request.Context(), db.CreateSessionParams{
		UserID:    user.ID,
		Token:     sessionToken,
		ExpiresAt: time.Now().UTC().Add(2 * time.Minute),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": sessionToken})
}

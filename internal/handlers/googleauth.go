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

	if token, err := c.Cookie("session_token"); err == nil {
		session, err := h.queries.GetSessionByToken(c.Request.Context(), token)
		if err == nil {
			c.JSON(http.StatusOK, gin.H{
				"status": "already authenticated",
				"data": gin.H{
					"session_id": session.ID,
					"expires_at": session.ExpiresAt,
				},
			})
			return
		}
	}

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

	existing, err := h.queries.GetActiveSessionByUserID(c.Request.Context(), user.ID)
	if err == nil {
		maxAge := max(1, int(time.Until(existing.ExpiresAt).Seconds()))
		c.SetCookie("session_token", existing.Token, maxAge, "/", "", false, true)
		c.JSON(http.StatusOK, gin.H{
			"token":      existing.Token,
			"expires_at": existing.ExpiresAt,
		})
		return
	}

	session, err := h.auth.CreateSession(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create session"})
		return
	}

	c.SetCookie("session_token", session.Token, int(h.sessionTTL.Seconds()), "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{
		"token":      session.Token,
		"expires_at": session.ExpiresAt,
	})
}

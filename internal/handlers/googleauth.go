package handlers

import (
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type AuthHandler struct {
	oauthCfg *oauth2.Config
}

func NewAuthHandler(clientID, clientSecret, redirectURL string) *AuthHandler {
	return &AuthHandler{
		oauthCfg: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		},
	}
}

// func (h *AuthHandler) GoogleLogin(c *gin.Context) {
// 	state := generateState()
// 	url := h.oauthCfg.AuthCodeURL(state, oauth2.AccessTypeOnline)
// 	c.Redirect(http.StatusTemporaryRedirect, url)
// }

// func generateState() string {
// 	b := make([]byte, 16)
// 	rand.Read(b)
// 	return base64.URLEncoding.EncodeToString(b)
// }

package handlers

import (
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type AuthHandler struct {
	oauthCfg *oauth2.Config
}

func NewAuthHandler(clientID, clientSecret, redirecURL string) *AuthHandler {
	return &AuthHandler{
		oauthCfg: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirecURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint:     google.Endpoint,
		},
	}
}

// func (h *AuthHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
// 	state := generateState()
// }

// func generateState() string {
// 	b := make([]byte, 16)
// 	rand.Read(b)
// 	return base64.URLEncoding.EncodeToString(b)
// }

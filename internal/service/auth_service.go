package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"

	"golang.org/x/oauth2"
)

type googleUserInfo struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func FetchGoogleUserInfo(
	ctx context.Context,
	cfg *oauth2.Config,
	token *oauth2.Token) (*googleUserInfo, error) {

	client := cfg.Client(ctx, token)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var userInfo googleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, err
	}
	return &userInfo, nil
}

func GenerateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b) // crypto/rand, not math/rand
	return base64.URLEncoding.EncodeToString(b)
}

func GenerateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

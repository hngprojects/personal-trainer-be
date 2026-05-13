package zoom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	accountID    string
	clientID     string
	clientSecret string
	httpClient   *http.Client
}

func New(accountID, clientID, clientSecret string) *Client {
	return &Client{
		accountID:    accountID,
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *Client) IsConfigured() bool {
	return c.accountID != "" && c.clientID != "" && c.clientSecret != ""
}

func (c *Client) getAccessToken(ctx context.Context) (string, error) {
	url := fmt.Sprintf("https://zoom.us/oauth/token?grant_type=account_credentials&account_id=%s", c.accountID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(c.clientID, c.clientSecret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("zoom: empty access token in response")
	}
	return result.AccessToken, nil
}

// CreateMeeting implements meeting.Provider.
func (c *Client) CreateMeeting(ctx context.Context, topic string, startTime time.Time, durationMinutes int) (joinURL, meetingID string, err error) {
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return "", "", fmt.Errorf("zoom: get token: %w", err)
	}

	body := map[string]interface{}{
		"topic":      topic,
		"type":       2,
		"start_time": startTime.UTC().Format("2006-01-02T15:04:05Z"),
		"duration":   durationMinutes,
		"settings": map[string]interface{}{
			"join_before_host": true,
			"waiting_room":     false,
		},
	}

	b, err := json.Marshal(body)
	if err != nil {
		return "", "", fmt.Errorf("zoom: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.zoom.us/v2/users/me/meetings", bytes.NewReader(b))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("zoom: create meeting: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("zoom: create meeting failed (%d): %s", resp.StatusCode, string(raw))
	}

	var m struct {
		ID      int64  `json:"id"`
		JoinURL string `json:"join_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return "", "", fmt.Errorf("zoom: decode response: %w", err)
	}
	return m.JoinURL, fmt.Sprintf("%d", m.ID), nil
}

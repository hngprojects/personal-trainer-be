package zoom

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTokenURL     = "https://zoom.us/oauth/token"
	defaultAPIBaseURL   = "https://api.zoom.us/v2"
	defaultUserID       = "me"
	defaultHTTPTimeout  = 10 * time.Second
	defaultRetryAttempts = 4
	defaultRetryBase     = 250 * time.Millisecond
	defaultRetryMax      = time.Second
)

type Client interface {
	CreateMeeting(ctx context.Context, input CreateMeetingInput) (CreateMeetingResult, error)
}

type Config struct {
	AccountID string
	ClientID  string
	Secret    string
	UserID    string

	TokenURL   string
	APIBaseURL string

	RetryMaxAttempts int
	RetryBaseDelay   time.Duration
	RetryMaxDelay    time.Duration

	HTTPClient *http.Client
}

type HTTPClient struct {
	accountID string
	clientID  string
	secret    string
	userID    string

	tokenURL   string
	apiBaseURL string

	retryMaxAttempts int
	retryBaseDelay   time.Duration
	retryMaxDelay    time.Duration

	httpClient *http.Client
}

type CreateMeetingInput struct {
	Topic           string
	StartTime       time.Time
	DurationMinutes int
	Timezone        string
	Agenda          string
}

type CreateMeetingResult struct {
	MeetingID string
	JoinURL   string
	StartURL  string
	Password  string
}

func NewClient(cfg Config) *HTTPClient {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}

	userID := strings.TrimSpace(cfg.UserID)
	if userID == "" {
		userID = defaultUserID
	}

	tokenURL := strings.TrimSpace(cfg.TokenURL)
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}

	apiBaseURL := strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/")
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIBaseURL
	}

	retryMaxAttempts := cfg.RetryMaxAttempts
	if retryMaxAttempts <= 0 {
		retryMaxAttempts = defaultRetryAttempts
	}

	retryBase := cfg.RetryBaseDelay
	if retryBase <= 0 {
		retryBase = defaultRetryBase
	}

	retryMax := cfg.RetryMaxDelay
	if retryMax <= 0 {
		retryMax = defaultRetryMax
	}
	if retryMax < retryBase {
		retryMax = retryBase
	}

	return &HTTPClient{
		accountID: cfg.AccountID,
		clientID:  cfg.ClientID,
		secret:    cfg.Secret,
		userID:    userID,
		tokenURL:  tokenURL,
		apiBaseURL: apiBaseURL,
		retryMaxAttempts: retryMaxAttempts,
		retryBaseDelay:   retryBase,
		retryMaxDelay:    retryMax,
		httpClient:       httpClient,
	}
}

func (c *HTTPClient) CreateMeeting(ctx context.Context, input CreateMeetingInput) (CreateMeetingResult, error) {
	if strings.TrimSpace(c.accountID) == "" || strings.TrimSpace(c.clientID) == "" || strings.TrimSpace(c.secret) == "" {
		return CreateMeetingResult{}, fmt.Errorf("zoom credentials are not configured")
	}

	if input.StartTime.IsZero() {
		return CreateMeetingResult{}, fmt.Errorf("start time is required")
	}

	if input.DurationMinutes <= 0 {
		input.DurationMinutes = 30
	}
	if strings.TrimSpace(input.Topic) == "" {
		input.Topic = "Discovery Call"
	}
	if strings.TrimSpace(input.Timezone) == "" {
		input.Timezone = "UTC"
	}

	payload, err := json.Marshal(zoomCreateMeetingRequest{
		Topic:     input.Topic,
		Type:      2,
		StartTime: input.StartTime.Format(time.RFC3339),
		Duration:  input.DurationMinutes,
		Timezone:  input.Timezone,
		Agenda:    input.Agenda,
	})
	if err != nil {
		return CreateMeetingResult{}, fmt.Errorf("marshal zoom meeting payload: %w", err)
	}

	var out CreateMeetingResult
	err = c.withRetry(ctx, func(ctx context.Context) (bool, error) {
		token, retryable, tokenErr := c.fetchAccessToken(ctx)
		if tokenErr != nil {
			return retryable, tokenErr
		}

		result, retryable, createErr := c.createMeetingWithToken(ctx, token, payload)
		if createErr != nil {
			return retryable, createErr
		}

		out = result
		return false, nil
	})
	if err != nil {
		return CreateMeetingResult{}, err
	}

	return out, nil
}

func (c *HTTPClient) fetchAccessToken(ctx context.Context) (string, bool, error) {
	query := url.Values{}
	query.Set("grant_type", "account_credentials")
	query.Set("account_id", c.accountID)

	endpoint := c.tokenURL
	if strings.Contains(endpoint, "?") {
		endpoint += "&" + query.Encode()
	} else {
		endpoint += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", false, fmt.Errorf("build zoom token request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(c.clientID + ":" + c.secret))
	req.Header.Set("Authorization", "Basic "+auth)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", isRetryableTransportError(err), fmt.Errorf("zoom token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", isRetryableStatus(resp.StatusCode), fmt.Errorf("zoom token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var tokenResp zoomTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", false, fmt.Errorf("decode zoom token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", false, fmt.Errorf("zoom token response missing access_token")
	}
	return tokenResp.AccessToken, false, nil
}

func (c *HTTPClient) createMeetingWithToken(ctx context.Context, token string, payload []byte) (CreateMeetingResult, bool, error) {
	endpoint := fmt.Sprintf("%s/users/%s/meetings", c.apiBaseURL, url.PathEscape(c.userID))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return CreateMeetingResult{}, false, fmt.Errorf("build zoom create meeting request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return CreateMeetingResult{}, isRetryableTransportError(err), fmt.Errorf("zoom create meeting request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CreateMeetingResult{}, isRetryableStatus(resp.StatusCode), fmt.Errorf("zoom create meeting failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var meetingResp zoomCreateMeetingResponse
	if err := json.Unmarshal(body, &meetingResp); err != nil {
		return CreateMeetingResult{}, false, fmt.Errorf("decode zoom create meeting response: %w", err)
	}

	return CreateMeetingResult{
		MeetingID: meetingResp.ID,
		JoinURL:   meetingResp.JoinURL,
		StartURL:  meetingResp.StartURL,
		Password:  meetingResp.Password,
	}, false, nil
}

func (c *HTTPClient) withRetry(ctx context.Context, fn func(context.Context) (bool, error)) error {
	var lastErr error

	for attempt := 0; attempt < c.retryMaxAttempts; attempt++ {
		retryable, err := fn(ctx)
		if err == nil {
			return nil
		}

		lastErr = err
		if !retryable || attempt == c.retryMaxAttempts-1 {
			return err
		}

		delay := backoffDelay(c.retryBaseDelay, c.retryMaxDelay, attempt)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}

	return lastErr
}

func backoffDelay(base, max time.Duration, attempt int) time.Duration {
	delay := base
	for i := 0; i < attempt; i++ {
		if delay >= max/2 {
			return max
		}
		delay *= 2
	}
	if delay > max {
		return max
	}
	return delay
}

func isRetryableStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}

func isRetryableTransportError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}

	if strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return true
	}

	return false
}

type zoomTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type zoomCreateMeetingRequest struct {
	Topic     string `json:"topic"`
	Type      int    `json:"type"`
	StartTime string `json:"start_time"`
	Duration  int    `json:"duration"`
	Timezone  string `json:"timezone"`
	Agenda    string `json:"agenda,omitempty"`
}

type zoomCreateMeetingResponse struct {
	ID       string `json:"id"`
	JoinURL  string `json:"join_url"`
	StartURL string `json:"start_url"`
	Password string `json:"password"`
}

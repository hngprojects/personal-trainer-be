package zoom

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCreateMeetingRetriesOnTransientCreateFailure(t *testing.T) {
	t.Parallel()

	var createCalls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"token-1"}`))
			return
		case "/v2/users/me/meetings":
			call := atomic.AddInt32(&createCalls, 1)
			if call < 3 {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"message":"temporary error"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"987654321","join_url":"https://zoom.us/j/987654321","start_url":"https://zoom.us/s/987654321","password":"abc123"}`))
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(Config{
		AccountID:        "acc-1",
		ClientID:         "client-1",
		Secret:           "secret-1",
		UserID:           "me",
		TokenURL:         server.URL + "/oauth/token",
		APIBaseURL:       server.URL + "/v2",
		RetryMaxAttempts: 4,
		RetryBaseDelay:   time.Millisecond,
		RetryMaxDelay:    2 * time.Millisecond,
		HTTPClient:       server.Client(),
	})

	out, err := client.CreateMeeting(context.Background(), CreateMeetingInput{
		Topic:           "Discovery",
		StartTime:       time.Now().UTC().Add(time.Hour),
		DurationMinutes: 30,
		Timezone:        "UTC",
	})
	if err != nil {
		t.Fatalf("expected success after transient retries, got err: %v", err)
	}

	if out.MeetingID == "" || out.JoinURL == "" || out.StartURL == "" {
		t.Fatalf("expected meeting payload, got: %+v", out)
	}

	if got := atomic.LoadInt32(&createCalls); got != 3 {
		t.Fatalf("expected 3 create attempts, got %d", got)
	}
}

func TestCreateMeetingDoesNotRetryPermanentCreateFailure(t *testing.T) {
	t.Parallel()

	var createCalls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"token-1"}`))
			return
		case "/v2/users/me/meetings":
			atomic.AddInt32(&createCalls, 1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"invalid payload"}`))
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(Config{
		AccountID:        "acc-1",
		ClientID:         "client-1",
		Secret:           "secret-1",
		UserID:           "me",
		TokenURL:         server.URL + "/oauth/token",
		APIBaseURL:       server.URL + "/v2",
		RetryMaxAttempts: 4,
		RetryBaseDelay:   time.Millisecond,
		RetryMaxDelay:    2 * time.Millisecond,
		HTTPClient:       server.Client(),
	})

	_, err := client.CreateMeeting(context.Background(), CreateMeetingInput{
		Topic:           "Discovery",
		StartTime:       time.Now().UTC().Add(time.Hour),
		DurationMinutes: 30,
		Timezone:        "UTC",
	})
	if err == nil {
		t.Fatal("expected permanent 400 error")
	}

	if got := atomic.LoadInt32(&createCalls); got != 1 {
		t.Fatalf("expected 1 create attempt on permanent error, got %d", got)
	}
}

func TestCreateMeetingRetriesTokenOnTransientFailure(t *testing.T) {
	t.Parallel()

	var tokenCalls int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			call := atomic.AddInt32(&tokenCalls, 1)
			if call == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"message":"rate limited"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"token-1"}`))
			return
		case "/v2/users/me/meetings":
			w.Header().Set("Content-Type", "application/json")
			resp := zoomCreateMeetingResponse{
				ID:       "123",
				JoinURL:  "https://zoom.us/j/123",
				StartURL: "https://zoom.us/s/123",
				Password: "pass",
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(Config{
		AccountID:        "acc-1",
		ClientID:         "client-1",
		Secret:           "secret-1",
		UserID:           "me",
		TokenURL:         server.URL + "/oauth/token",
		APIBaseURL:       server.URL + "/v2",
		RetryMaxAttempts: 4,
		RetryBaseDelay:   time.Millisecond,
		RetryMaxDelay:    2 * time.Millisecond,
		HTTPClient:       server.Client(),
	})

	_, err := client.CreateMeeting(context.Background(), CreateMeetingInput{
		Topic:           "Discovery",
		StartTime:       time.Now().UTC().Add(time.Hour),
		DurationMinutes: 30,
		Timezone:        "UTC",
	})
	if err != nil {
		t.Fatalf("expected success after token retry, got err: %v", err)
	}

	if got := atomic.LoadInt32(&tokenCalls); got != 2 {
		t.Fatalf("expected 2 token calls, got %d", got)
	}
}

package email

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const resendEndpoint = "https://api.resend.com/emails"

// ResendMailer sends transactional email via the Resend HTTP API
// (https://resend.com/docs/api-reference/emails/send-email).
type ResendMailer struct {
	apiKey string
	from   string
	client *http.Client
}

func NewResendMailer(apiKey, from string) *ResendMailer {
	return &ResendMailer{
		apiKey: apiKey,
		from:   from,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
}

// resendError matches Resend's error envelope so we can surface a useful
// message when the API rejects a request.
type resendError struct {
	Name    string `json:"name"`
	Message string `json:"message"`
}

func (m *ResendMailer) SendVerificationCode(to, code string, expiryMinutes int) error {
	body, err := verificationCodeHTML(code, expiryMinutes)
	if err != nil {
		return fmt.Errorf("resend: build verification email body: %w", err)
	}
	return m.send(to, verificationCodeSubject, body)
}

func (m *ResendMailer) SendAdminCredentials(to, password string) error {
	body, err := adminCredentialsHTML(to, password)
	if err != nil {
		return fmt.Errorf("resend: build admin credentials email body: %w", err)
	}
	return m.send(to, adminCredentialsSubject, body)
}

func (m *ResendMailer) SendTrainerCredentials(to, password string) error {
	// Fastest: reuse the admin HTML template but with a trainer-ish subject.
	body, err := adminCredentialsHTML(to, password)
	if err != nil {
		return err
	}
	return m.send(to, "Your trainer account is ready", body)
}

func (m *ResendMailer) SendPasswordResetCode(to, code string, expiryMinutes int) error {
	body, err := passwordResetHTML(code, expiryMinutes)
	if err != nil {
		return fmt.Errorf("resend: build password reset email body: %w", err)
	}
	return m.send(to, passwordResetSubject, body)
}

func (m *ResendMailer) SendWaitlistConfirmation(to string) error {
	body, err := waitlistConfirmationHTML()
	if err != nil {
		return fmt.Errorf("resend: build waitlist confirmation email body: %w", err)
	}
	return m.send(to, waitlistConfirmationSubject, body)
}

func (m *ResendMailer) send(to, subject, htmlBody string) error {
	payload, err := json.Marshal(resendRequest{
		From:    m.from,
		To:      []string{to},
		Subject: subject,
		HTML:    htmlBody,
	})
	if err != nil {
		return fmt.Errorf("resend: marshal payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, resendEndpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("resend: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: http call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	var apiErr resendError
	if json.Unmarshal(body, &apiErr) == nil && apiErr.Message != "" {
		return fmt.Errorf("resend: %s (%s)", apiErr.Message, apiErr.Name)
	}
	return fmt.Errorf("resend: unexpected status %d: %s", resp.StatusCode, string(body))
}

package email

import (
	"bytes"
	"encoding/json"
	"html/template"
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
	body, err := trainerCredentialsHTML(to, password)
	if err != nil {
		return fmt.Errorf("resend: build trainer credentials email body: %w", err)
	}
	return m.send(to, trainerCredentialsSubject, body)
}

func (m *ResendMailer) SendAccountSetupLink(to, name, link string, expiryHours int) error {
	body, err := accountSetupHTML(name, link, expiryHours)
	if err != nil {
		return fmt.Errorf("resend: build account setup email body: %w", err)
	}
	return m.send(to, accountSetupSubject, body)
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

func (m *ResendMailer) SendDiscoveryBookingConfirmation(to, name string, scheduledAt time.Time, timezone, contactMode, phoneNumber, zoomLink string) error {
	html, err := discoveryBookingHTML(name, scheduledAt, timezone, contactMode, phoneNumber, zoomLink)
	if err != nil {
		return fmt.Errorf("resend: build discovery booking email: %w", err)
	}
	subject := zoomMeetingConfirmationSubject
	if contactMode == "phone_callback" {
		subject = phoneCallConfirmationSubject
	}
	return m.send(to, subject, html)
}

func (m *ResendMailer) SendDiscoveryBookingAdminNotification(to, clientName, clientEmail string, scheduledAt time.Time, timezone, contactMode, phoneNumber, zoomLink string) error {
	html, err := discoveryBookingAdminHTML(clientName, clientEmail, scheduledAt, timezone, contactMode, phoneNumber, zoomLink)
	if err != nil {
		return fmt.Errorf("resend: build admin notification email: %w", err)
	}
	return m.send(to, discoveryBookingAdminNotificationSubject, html)
}

func (m *ResendMailer) SendContactConfirmation(to, name string) error {
	body, err := contactConfirmationHTML(name)
	if err != nil {
		return fmt.Errorf("resend: build contact confirmation email body: %w", err)
	}
	return m.send(to, contactConfirmationSubject, body)
}

func (m *ResendMailer) SendDiscoveryRescheduleConfirmation(to, name string, oldTime, newTime time.Time, timezone, contactMode, phoneNumber, zoomLink string) error {
	html, err := discoveryRescheduleHTML(name, oldTime, newTime, timezone, contactMode, phoneNumber, zoomLink)
	if err != nil {
		return fmt.Errorf("resend: build reschedule email: %w", err)
	}
	return m.send(to, discoveryRescheduleSubject, html)
}

func (m *ResendMailer) SendPaidSessionRescheduleConfirmation(to, name string, oldTime, newTime time.Time, timezone, zoomLink string) error {
	html, err := paidRescheduleClientHTML(name, oldTime, newTime, timezone, zoomLink)
	if err != nil {
		return fmt.Errorf("resend: build paid session reschedule email: %w", err)
	}
	return m.send(to, paidRescheduleClientSubject, html)
}

func (m *ResendMailer) SendPaidSessionRescheduleTrainerNotification(to, clientName string, oldTime, newTime time.Time, timezone, zoomLink string) error {
	html, err := paidRescheduleTrainerHTML(clientName, oldTime, newTime, timezone, zoomLink)
	if err != nil {
		return fmt.Errorf("resend: build paid session reschedule trainer notification email: %w", err)
	}
	return m.send(to, paidRescheduleTrainerSubject, html)
}

func (m *ResendMailer) SendBookingConfirmation(to, name, trainerName string, scheduledStartTime, scheduledEndTime time.Time, timezone string, zoomLink string) error {
	html, err := bookingConfirmation(name, trainerName, scheduledStartTime, scheduledEndTime, timezone, zoomLink)
	if err != nil {
		return fmt.Errorf("resend: build booking confirmation email: %w", err)
	}
	return m.send(to, bookingConfirmationSubject, html)
}

func (m *ResendMailer) SendBookingCancellation(to, recipientName, otherPartyName string, scheduledStart time.Time, timezone, reason string) error {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	t, err := template.New("booking-cancellation").Parse(bookingCancellationTemplate)
	if err != nil {
		return fmt.Errorf("resend: build cancellation email: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, map[string]interface{}{
		"RecipientName":  recipientName,
		"OtherRole":      "Session with",
		"OtherPartyName": otherPartyName,
		"Date":           scheduledStart.In(loc).Format("Monday, January 2, 2006 at 3:04 PM"),
		"Reason":         reason,
	}); err != nil {
		return fmt.Errorf("resend: render cancellation email: %w", err)
	}
	return m.send(to, bookingCancellationSubject, buf.String())
}

func (m *ResendMailer) SendSessionComplete(to, clientName, trainerName string) error {
	t, err := template.New("session-complete").Parse(sessionCompleteTemplate)
	if err != nil {
		return fmt.Errorf("resend: build session complete email: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, map[string]interface{}{
		"ClientName":  clientName,
		"TrainerName": trainerName,
	}); err != nil {
		return fmt.Errorf("resend: render session complete email: %w", err)
	}
	return m.send(to, sessionCompleteSubject, buf.String())
}

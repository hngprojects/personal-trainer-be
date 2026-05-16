package email

import (
	"bytes"
	"fmt"
	"html/template"
	"log/slog"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type Mailer interface {
	SendVerificationCode(to, code string, expiryMinutes int) error
	SendAdminCredentials(to, password string) error
	SendPasswordResetCode(to, code string, expiryMinutes int) error
	SendWaitlistConfirmation(to string) error
	SendContactConfirmation(to, name string) error
	SendDiscoveryBookingConfirmation(to, name string, scheduledAt time.Time, timezone, contactMode, phoneNumber, zoomLink string) error
	SendDiscoveryBookingAdminNotification(to, clientName, clientEmail string, scheduledAt time.Time, timezone, contactMode, phoneNumber, zoomLink string) error
	SendDiscoveryRescheduleConfirmation(to, name string, oldTime, newTime time.Time, timezone, contactMode, phoneNumber, zoomLink string) error
	SendPaidSessionRescheduleConfirmation(to, name string, oldTime, newTime time.Time, timezone, zoomLink string) error
	SendPaidSessionRescheduleTrainerNotification(to, clientName string, oldTime, newTime time.Time, timezone, zoomLink string) error
	SendBookingConfirmation(to, clientName, trainerName string, scheduledStartTime, scheduledEndTime time.Time, timezone string, zoomLink string) error
}

type SMTPMailer struct {
	host     string
	port     string
	username string
	password string
	from     string
}

func NewSMTPMailer(host, port, username, password, from string) *SMTPMailer {
	return &SMTPMailer{host: host, port: port, username: username, password: password, from: from}
}

func (m *SMTPMailer) SendVerificationCode(to, code string, expiryMinutes int) error {
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	body, err := verificationCodeHTML(code, expiryMinutes)
	if err != nil {
		return fmt.Errorf("build verification email body: %w", err)
	}

	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, verificationCodeSubject, body,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

func (m *SMTPMailer) SendAdminCredentials(to, password string) error {
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	body, err := adminCredentialsHTML(toAddr, password)
	if err != nil {
		return fmt.Errorf("build admin credentials email body: %w", err)
	}
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, adminCredentialsSubject, body,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

func (m *SMTPMailer) SendPasswordResetCode(to, code string, expiryMinutes int) error {
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	body, err := passwordResetHTML(code, expiryMinutes)
	if err != nil {
		return fmt.Errorf("build password reset email body: %w", err)
	}

	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, passwordResetSubject, body,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

// LogMailer logs emails instead of sending them — useful in development.
type LogMailer struct{}

func NewLogMailer() *LogMailer { return &LogMailer{} }

func (m *LogMailer) SendVerificationCode(to, code string, expiryMinutes int) error {
	slog.Info("email (verification code redacted)",
		"to", to,
		"subject", verificationCodeSubject,
		"expires_in_minutes", expiryMinutes,
	)
	return nil
}

func (m *LogMailer) SendAdminCredentials(to, password string) error {
	body, err := adminCredentialsHTML(to, password)
	if err != nil {
		return err
	}
	slog.Info("email", "to", to, "subject", adminCredentialsSubject, "body", body)
	return nil
}

// SendPasswordResetCode logs only metadata — never the rendered body, which
// contains the live reset code. If the LogMailer is ever enabled outside an
// isolated local workflow, anyone with log access could otherwise reset
// accounts. Local E2E flows that need the actual code should use a test stub
// (e.g. a fake mailer that captures the args), not the LogMailer.
func (m *LogMailer) SendPasswordResetCode(to, code string, expiryMinutes int) error {
	slog.Info("email (password reset code redacted)",
		"to", to,
		"subject", passwordResetSubject,
		"expires_in_minutes", expiryMinutes,
	)
	return nil
}

func (m *LogMailer) SendWaitlistConfirmation(to string) error {
	slog.Info("email", "to", to, "subject", waitlistConfirmationSubject)
	return nil
}

func (m *LogMailer) SendDiscoveryBookingConfirmation(to, name string, scheduledAt time.Time, timezone, contactMode, phoneNumber, zoomLink string) error {
	subject := zoomMeetingConfirmationSubject
	if contactMode == "phone_callback" {
		subject = phoneCallConfirmationSubject
	}
	slog.Info("email", "to", to, "subject", subject, "name", name, "contact_mode", contactMode)
	return nil
}

func (m *LogMailer) SendDiscoveryBookingAdminNotification(to, clientName, clientEmail string, scheduledAt time.Time, timezone, contactMode, phoneNumber, zoomLink string) error {
	slog.Info("email (admin discovery notification)", "to", to, "client", clientName)
	return nil
}

func (m *LogMailer) SendDiscoveryRescheduleConfirmation(to, name string, oldTime, newTime time.Time, timezone, contactMode, phoneNumber, zoomLink string) error {
	slog.Info("email (discovery reschedule)", "to", to, "name", name, "new_time", newTime)
	return nil
}

func (m *LogMailer) SendPaidSessionRescheduleConfirmation(to, name string, oldTime, newTime time.Time, timezone, zoomLink string) error {
	slog.Info("email (paid session reschedule)", "to", to, "name", name, "new_time", newTime)
	return nil
}

func (m *LogMailer) SendPaidSessionRescheduleTrainerNotification(to, clientName string, oldTime, newTime time.Time, timezone, zoomLink string) error {
	slog.Info("email (paid session reschedule trainer notification)", "to", to, "client", clientName, "new_time", newTime)
	return nil
}

func (m *LogMailer) SendContactConfirmation(to, _ string) error {
	slog.Info("email", "to", to, "subject", contactConfirmationSubject)
	return nil
}

func (m *LogMailer) SendBookingConfirmation(to, clientName, trainerName string, scheduledStartTime, scheduledEndTime time.Time, timezone string, zoomLink string) error {
	slog.Info("email (booking confirmation)", "to", to, "client", clientName, "start", scheduledStartTime, "end", scheduledEndTime, "timezone", timezone, "zoom_link", zoomLink)
	return nil
}

const phoneCallConfirmationSubject = "Your FitCall Discovery Call is Confirmed"

var phoneCallConfirmationTemplate = template.Must(template.New("phone-call-confirmation").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1.0">
</head>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:Arial,sans-serif;">
  <table width="100%" cellpadding="0" cellspacing="0" style="padding:40px 0;">
    <tr><td align="center">
      <table width="520" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:8px;padding:40px;">
        <tr><td style="padding-bottom:24px;">
          <h2 style="margin:0;font-size:22px;color:#111827;">Discovery Call Confirmed</h2>
        </td></tr>
        <tr><td style="padding-bottom:20px;">
          <p style="margin:0;font-size:15px;color:#374151;">Hello <strong>{{ .Name }}</strong>,</p>
        </td></tr>
        <tr><td style="padding-bottom:24px;">
          <p style="margin:0;font-size:14px;color:#6b7280;">Your phone discovery call has been successfully scheduled.</p>
        </td></tr>
        <tr><td style="padding:20px;background:#f9fafb;border-radius:8px;padding-bottom:24px;">
          <p style="margin:0 0 10px;font-size:14px;color:#374151;">📅 <strong>Date:</strong> {{ .Date }}</p>
          <p style="margin:0 0 10px;font-size:14px;color:#374151;">🕒 <strong>Time:</strong> {{ .Time }} ({{ .Timezone }})</p>
          <p style="margin:0;font-size:14px;color:#374151;">📞 <strong>Phone Number:</strong> {{ .PhoneNumber }}</p>
        </td></tr>
        <tr><td style="padding-top:24px;">
          <p style="margin:0;font-size:14px;color:#6b7280;">A member of our team will call you at the scheduled time.</p>
        </td></tr>
        <tr><td style="padding-top:16px;">
          <p style="margin:0;font-size:14px;color:#6b7280;">If you need to make any changes to your booking, simply reply to this email.</p>
        </td></tr>
        <tr><td style="padding-top:24px;">
          <p style="margin:0;font-size:14px;color:#374151;">Best regards,<br><strong>The FitCall Team</strong></p>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`))

const zoomMeetingConfirmationSubject = "Your FitCall Discovery Call is Confirmed"

var zoomMeetingConfirmationTemplate = template.Must(template.New("zoom-meeting-confirmation").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1.0">
</head>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:Arial,sans-serif;">
  <table width="100%" cellpadding="0" cellspacing="0" style="padding:40px 0;">
    <tr><td align="center">
      <table width="520" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:8px;padding:40px;">
        <tr><td style="padding-bottom:24px;">
          <h2 style="margin:0;font-size:22px;color:#111827;">Discovery Call Confirmed</h2>
        </td></tr>
        <tr><td style="padding-bottom:20px;">
          <p style="margin:0;font-size:15px;color:#374151;">Hello <strong>{{ .Name }}</strong>,</p>
        </td></tr>
        <tr><td style="padding-bottom:24px;">
          <p style="margin:0;font-size:14px;color:#6b7280;">Your Zoom discovery call has been successfully scheduled.</p>
        </td></tr>
        <tr><td style="padding:20px;background:#f9fafb;border-radius:8px;padding-bottom:24px;">
          <p style="margin:0 0 10px;font-size:14px;color:#374151;">📅 <strong>Date:</strong> {{ .Date }}</p>
          <p style="margin:0 0 10px;font-size:14px;color:#374151;">🕒 <strong>Time:</strong> {{ .Time }} ({{ .Timezone }})</p>
          <p style="margin:0;font-size:14px;color:#374151;">🔗 <strong>Zoom Link:</strong> <a href="{{ .ZoomLink }}" style="color:#2563eb;">{{ .ZoomLink }}</a></p>
        </td></tr>
        <tr><td style="padding-top:24px;">
          <p style="margin:0;font-size:14px;color:#6b7280;">If you need to make any changes to your booking, simply reply to this email.</p>
        </td></tr>
        <tr><td style="padding-top:24px;">
          <p style="margin:0;font-size:14px;color:#374151;">Best regards,<br><strong>The FitCall Team</strong></p>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`))

const discoveryBookingAdminNotificationSubject = "New Discovery Call Booking"

var discoveryBookingAdminTemplate = template.Must(template.New("discovery-admin-notification").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1.0">
</head>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:Arial,sans-serif;">
  <table width="100%" cellpadding="0" cellspacing="0" style="padding:40px 0;">
    <tr><td align="center">
      <table width="520" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:8px;padding:40px;">
        <tr><td style="padding-bottom:24px;">
          <h2 style="margin:0;font-size:22px;color:#111827;">New Discovery Call Booked</h2>
        </td></tr>
        <tr><td style="padding:20px;background:#f9fafb;border-radius:8px;padding-bottom:24px;">
          <p style="margin:0 0 10px;font-size:14px;color:#374151;"><strong>Client:</strong> {{ .ClientName }}</p>
          <p style="margin:0 0 10px;font-size:14px;color:#374151;"><strong>Email:</strong> {{ .ClientEmail }}</p>
          <p style="margin:0 0 10px;font-size:14px;color:#374151;"><strong>Contact Mode:</strong> {{ .ContactMode }}</p>
          <p style="margin:0 0 10px;font-size:14px;color:#374151;"><strong>Date:</strong> {{ .Date }}</p>
          <p style="margin:0 0 10px;font-size:14px;color:#374151;"><strong>Time:</strong> {{ .Time }} ({{ .Timezone }})</p>
          {{ if .PhoneNumber }}<p style="margin:0 0 10px;font-size:14px;color:#374151;"><strong>Phone:</strong> {{ .PhoneNumber }}</p>{{ end }}
          {{ if .ZoomLink }}<p style="margin:0;font-size:14px;color:#374151;"><strong>Zoom Link:</strong> <a href="{{ .ZoomLink }}" style="color:#2563eb;">{{ .ZoomLink }}</a></p>{{ end }}
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`))

func discoveryBookingAdminHTML(clientName, clientEmail string, scheduledAt time.Time, timezone, contactMode, phoneNumber, zoomLink string) (string, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	local := scheduledAt.In(loc)
	var buf bytes.Buffer
	err = discoveryBookingAdminTemplate.Execute(&buf, struct {
		ClientName, ClientEmail, ContactMode, Date, Time, Timezone, PhoneNumber, ZoomLink string
	}{clientName, clientEmail, contactMode, local.Format("Monday, January 2, 2006"), local.Format("3:04 PM"), timezone, phoneNumber, zoomLink})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func discoveryBookingHTML(name string, scheduledAt time.Time, timezone, contactMode, phoneNumber, zoomLink string) (string, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	local := scheduledAt.In(loc)
	date := local.Format("Monday, January 2, 2006")
	t := local.Format("3:04 PM")

	var buf bytes.Buffer
	if contactMode == "phone_callback" {
		err = phoneCallConfirmationTemplate.Execute(&buf, struct {
			Name, Date, Time, Timezone, PhoneNumber string
		}{name, date, t, timezone, phoneNumber})
	} else {
		err = zoomMeetingConfirmationTemplate.Execute(&buf, struct {
			Name, Date, Time, Timezone, ZoomLink string
		}{name, date, t, timezone, zoomLink})
	}
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func (m *SMTPMailer) SendDiscoveryBookingConfirmation(to, name string, scheduledAt time.Time, timezone, contactMode, phoneNumber, zoomLink string) error {
	body, err := discoveryBookingHTML(name, scheduledAt, timezone, contactMode, phoneNumber, zoomLink)
	if err != nil {
		return fmt.Errorf("build discovery booking email body: %w", err)
	}
	subject := zoomMeetingConfirmationSubject
	if contactMode == "phone_callback" {
		subject = phoneCallConfirmationSubject
	}
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, subject, body,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

func (m *SMTPMailer) SendDiscoveryBookingAdminNotification(to, clientName, clientEmail string, scheduledAt time.Time, timezone, contactMode, phoneNumber, zoomLink string) error {
	body, err := discoveryBookingAdminHTML(clientName, clientEmail, scheduledAt, timezone, contactMode, phoneNumber, zoomLink)
	if err != nil {
		return fmt.Errorf("build discovery booking admin email body: %w", err)
	}
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, discoveryBookingAdminNotificationSubject, body,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

func sanitizeAddress(value string) (string, error) {
	safeValue, err := sanitizeHeaderValue(value)
	if err != nil {
		return "", err
	}

	addr, err := mail.ParseAddress(safeValue)
	if err != nil {
		return "", err
	}

	return addr.Address, nil
}

func sanitizeHeaderValue(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("value is required")
	}
	if strings.ContainsAny(trimmed, "\r\n") {
		return "", fmt.Errorf("value contains newline characters")
	}
	return trimmed, nil
}

const verificationCodeSubject = "Your verification code"

var verificationCodeTemplate = template.Must(template.New("verification-code-email").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1.0">
</head>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:Arial,sans-serif;">
  <table width="100%" cellpadding="0" cellspacing="0" style="padding:40px 0;">
    <tr><td align="center">
      <table width="480" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:8px;padding:40px;">
        <tr><td align="center" style="padding-bottom:16px;">
          <h2 style="margin:0;font-size:22px;color:#111827;">Your Verification Code</h2>
        </td></tr>
        <tr><td align="center" style="padding-bottom:24px;">
          <p style="margin:0;font-size:14px;color:#6b7280;">Use the code below to verify your email address.</p>
        </td></tr>
        <tr><td align="center" style="padding:24px 0;">
          <span style="display:inline-block;font-size:36px;font-weight:bold;letter-spacing:8px;color:#111827;background:#f4f4f5;padding:16px 32px;border-radius:8px;">{{ .Code }}</span>
        </td></tr>
        <tr><td align="center" style="padding-top:16px;">
          <p style="margin:0;font-size:12px;color:#9ca3af;">Expires in {{ .ExpiryMinutes }} minutes. Do not share this code with anyone.</p>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`))

func verificationCodeHTML(code string, expiryMinutes int) (string, error) {
	var body bytes.Buffer
	if err := verificationCodeTemplate.Execute(&body, struct {
		Code          string
		ExpiryMinutes int
	}{
		Code:          code,
		ExpiryMinutes: expiryMinutes,
	}); err != nil {
		return "", err
	}
	return body.String(), nil
}

const adminCredentialsSubject = "Your admin account is ready"

var adminCredentialsTemplate = template.Must(template.New("admin-credentials-email").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1.0">
</head>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:Arial,sans-serif;">
  <table width="100%" cellpadding="0" cellspacing="0" style="padding:40px 0;">
    <tr><td align="center">
      <table width="480" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:8px;padding:40px;">
        <tr><td align="center" style="padding-bottom:16px;">
          <h2 style="margin:0;font-size:22px;color:#111827;">Your admin account is ready</h2>
        </td></tr>
        <tr><td align="center" style="padding-bottom:24px;">
          <p style="margin:0;font-size:14px;color:#6b7280;">An admin account was created for you. Use the credentials below to sign in.</p>
        </td></tr>
        <tr><td style="padding:8px 0;">
          <p style="margin:0;font-size:14px;color:#111827;"><strong>Email:</strong> {{ .Email }}</p>
        </td></tr>
        <tr><td style="padding:8px 0 24px;">
          <p style="margin:0;font-size:14px;color:#111827;"><strong>Temporary password:</strong>
            <span style="font-family:monospace;background:#f4f4f5;padding:4px 8px;border-radius:4px;">{{ .Password }}</span>
          </p>
        </td></tr>
        <tr><td align="center" style="padding-top:16px;">
          <p style="margin:0;font-size:12px;color:#9ca3af;">Please change this password after your first sign-in. Do not share this email with anyone.</p>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`))

func adminCredentialsHTML(emailAddr, password string) (string, error) {
	var body bytes.Buffer
	if err := adminCredentialsTemplate.Execute(&body, struct {
		Email    string
		Password string
	}{
		Email:    emailAddr,
		Password: password,
	}); err != nil {
		return "", err
	}
	return body.String(), nil
}

const passwordResetSubject = "Your password reset code"

var passwordResetTemplate = template.Must(template.New("password-reset-email").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1.0">
</head>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:Arial,sans-serif;">
  <table width="100%" cellpadding="0" cellspacing="0" style="padding:40px 0;">
    <tr><td align="center">
      <table width="480" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:8px;padding:40px;">
        <tr><td align="center" style="padding-bottom:16px;">
          <h2 style="margin:0;font-size:22px;color:#111827;">Reset your password</h2>
        </td></tr>
        <tr><td align="center" style="padding-bottom:24px;">
          <p style="margin:0;font-size:14px;color:#6b7280;">Use the code below to reset your password. If you did not request this, ignore this email.</p>
        </td></tr>
        <tr><td align="center" style="padding:24px 0;">
          <span style="display:inline-block;font-size:36px;font-weight:bold;letter-spacing:8px;color:#111827;background:#f4f4f5;padding:16px 32px;border-radius:8px;">{{ .Code }}</span>
        </td></tr>
        <tr><td align="center" style="padding-top:16px;">
          <p style="margin:0;font-size:12px;color:#9ca3af;">Expires in {{ .ExpiryMinutes }} minutes. Do not share this code with anyone.</p>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`))

func passwordResetHTML(code string, expiryMinutes int) (string, error) {
	var body bytes.Buffer
	if err := passwordResetTemplate.Execute(&body, struct {
		Code          string
		ExpiryMinutes int
	}{
		Code:          code,
		ExpiryMinutes: expiryMinutes,
	}); err != nil {
		return "", err
	}
	return body.String(), nil
}

func (m *SMTPMailer) SendWaitlistConfirmation(to string) error {
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	body, err := waitlistConfirmationHTML()
	if err != nil {
		return fmt.Errorf("build waitlist confirmation email body: %w", err)
	}
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, waitlistConfirmationSubject, body,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

func (m *SMTPMailer) SendContactConfirmation(to, name string) error {
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	body, err := contactConfirmationHTML(name)
	if err != nil {
		return fmt.Errorf("build contact confirmation email body: %w", err)
	}
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, contactConfirmationSubject, body,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

const waitlistConfirmationSubject = "You're on the waitlist!"

var waitlistConfirmationTemplate = template.Must(template.New("waitlist-confirmation-email").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1.0">
</head>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:Arial,sans-serif;">
  <table width="100%" cellpadding="0" cellspacing="0" style="padding:40px 0;">
    <tr><td align="center">
      <table width="480" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:8px;padding:40px;">
        <tr><td align="center" style="padding-bottom:16px;">
          <h2 style="margin:0;font-size:22px;color:#111827;">You're on the waitlist!</h2>
        </td></tr>
        <tr><td align="center" style="padding-bottom:24px;">
          <p style="margin:0;font-size:14px;color:#6b7280;">Thanks for joining! We'll notify you as soon as you have access.</p>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`))

func waitlistConfirmationHTML() (string, error) {
	var body bytes.Buffer
	if err := waitlistConfirmationTemplate.Execute(&body, nil); err != nil {
		return "", err
	}
	return body.String(), nil
}

const contactConfirmationSubject = "We received your message!"

var contactConfirmationTemplate = template.Must(template.New("contact-confirmation-email").Parse(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1.0">
</head>
<body style="margin:0;padding:0;background:#f4f4f5;font-family:Arial,sans-serif;">
  <table width="100%" cellpadding="0" cellspacing="0" style="padding:40px 0;">
    <tr><td align="center">
      <table width="480" cellpadding="0" cellspacing="0" style="background:#ffffff;border-radius:8px;padding:40px;">
        <tr><td align="center" style="padding-bottom:16px;">
          <h2 style="margin:0;font-size:22px;color:#111827;">We received your message!</h2>
        </td></tr>
        <tr><td align="center" style="padding-bottom:24px;">
          <p style="margin:0;font-size:14px;color:#6b7280;">Hi {{ .Name }}, thanks for reaching out. We'll get back to you as soon as possible.</p>
        </td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`))

func contactConfirmationHTML(name string) (string, error) {
	var body bytes.Buffer
	if err := contactConfirmationTemplate.Execute(&body, struct{ Name string }{Name: name}); err != nil {
		return "", err
	}
	return body.String(), nil
}

func (m *SMTPMailer) SendDiscoveryRescheduleConfirmation(to, name string, oldTime, newTime time.Time, timezone, contactMode, phoneNumber, zoomLink string) error {
	html, err := discoveryRescheduleHTML(name, oldTime, newTime, timezone, contactMode, phoneNumber, zoomLink)
	if err != nil {
		return fmt.Errorf("smtp: build reschedule email: %w", err)
	}
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("smtp: invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("smtp: invalid recipient address: %w", err)
	}
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, discoveryRescheduleSubject, html,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

const discoveryRescheduleSubject = "Your Discovery Call Has Been Rescheduled"

func discoveryRescheduleHTML(name string, oldTime, newTime time.Time, timezone, contactMode, phoneNumber, zoomLink string) (string, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	oldLocal := oldTime.In(loc)
	newLocal := newTime.In(loc)

	t, err := template.New("reschedule").Parse(discoveryRescheduleTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]interface{}{
		"Name":        name,
		"OldTime":     oldLocal.Format("Monday, January 2, 2006 at 3:04 PM"),
		"NewTime":     newLocal.Format("Monday, January 2, 2006 at 3:04 PM"),
		"Timezone":    timezone,
		"ContactMode": contactMode,
		"ZoomLink":    zoomLink,
		"PhoneNumber": phoneNumber,
	})
	return buf.String(), err
}

const discoveryRescheduleTemplate = `<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>Discovery Call Rescheduled</title></head>
<body style="font-family:Arial,sans-serif;max-width:600px;margin:0 auto;padding:20px;">
  <h2 style="color:#1a1a2e;">Your Discovery Call Has Been Rescheduled</h2>
  <p>Hi {{.Name}},</p>
  <p>Your discovery call has been successfully rescheduled.</p>
  <table style="width:100%;border-collapse:collapse;margin:20px 0;">
    <tr>
      <td style="padding:10px;background:#f8d7da;border-radius:4px 0 0 4px;"><strong>Previous Time</strong><br>{{.OldTime}}</td>
      <td style="padding:10px;background:#d4edda;border-radius:0 4px 4px 0;"><strong>New Time</strong><br>{{.NewTime}}</td>
    </tr>
  </table>
  <p><strong>Timezone:</strong> {{.Timezone}}</p>
  {{ if eq .ContactMode "zoom_meeting" }}{{ if .ZoomLink }}<p><strong>New Zoom Link:</strong> <a href="{{.ZoomLink}}">{{.ZoomLink}}</a></p>{{ end }}{{ else if eq .ContactMode "phone_callback" }}{{ if .PhoneNumber }}<p><strong>Phone Number:</strong> {{.PhoneNumber}}</p>{{ end }}{{ end }}
  <p>If you need to make any further changes, please do so at least 12 hours before the scheduled time.</p>
  <p>See you soon!</p>
  <p style="color:#666;font-size:12px;">FitCall Team</p>
</body>
</html>`

func (m *SMTPMailer) SendPaidSessionRescheduleConfirmation(to, name string, oldTime, newTime time.Time, timezone, zoomLink string) error {
	html, err := paidRescheduleClientHTML(name, oldTime, newTime, timezone, zoomLink)
	if err != nil {
		return fmt.Errorf("smtp: build paid session reschedule email: %w", err)
	}
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("smtp: invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("smtp: invalid recipient address: %w", err)
	}
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, paidRescheduleClientSubject, html,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

func (m *SMTPMailer) SendPaidSessionRescheduleTrainerNotification(to, clientName string, oldTime, newTime time.Time, timezone, zoomLink string) error {
	html, err := paidRescheduleTrainerHTML(clientName, oldTime, newTime, timezone, zoomLink)
	if err != nil {
		return fmt.Errorf("smtp: build paid session reschedule trainer notification email: %w", err)
	}
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("smtp: invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("smtp: invalid recipient address: %w", err)
	}
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, paidRescheduleTrainerSubject, html,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

const paidRescheduleClientSubject = "Your Training Session Has Been Rescheduled"

const paidRescheduleTrainerSubject = "Client Rescheduled Training Session"

func paidRescheduleClientHTML(name string, oldTime, newTime time.Time, timezone, zoomLink string) (string, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	t, err := template.New("paid-client-reschedule").Parse(paidRescheduleClientTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]interface{}{
		"Name":     name,
		"OldTime":  oldTime.In(loc).Format("Monday, January 2, 2006 at 3:04 PM"),
		"NewTime":  newTime.In(loc).Format("Monday, January 2, 2006 at 3:04 PM"),
		"Timezone": timezone,
		"ZoomLink": zoomLink,
	})
	return buf.String(), err
}

func paidRescheduleTrainerHTML(clientName string, oldTime, newTime time.Time, timezone, zoomLink string) (string, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	t, err := template.New("paid-trainer-reschedule").Parse(paidRescheduleTrainerTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]interface{}{
		"ClientName": clientName,
		"OldTime":    oldTime.In(loc).Format("Monday, January 2, 2006 at 3:04 PM"),
		"NewTime":    newTime.In(loc).Format("Monday, January 2, 2006 at 3:04 PM"),
		"Timezone":   timezone,
		"ZoomLink":   zoomLink,
	})
	return buf.String(), err
}

const paidRescheduleClientTemplate = `<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>Training Session Rescheduled</title></head>
<body style="font-family:Arial,sans-serif;max-width:600px;margin:0 auto;padding:20px;">
  <h2 style="color:#1a1a2e;">Your Training Session Has Been Rescheduled</h2>
  <p>Hi {{.Name}},</p>
  <p>Your training session has been successfully rescheduled.</p>
  <table style="width:100%;border-collapse:collapse;margin:20px 0;">
    <tr>
      <td style="padding:10px;background:#f8d7da;border-radius:4px 0 0 4px;"><strong>Previous Time</strong><br>{{.OldTime}}</td>
      <td style="padding:10px;background:#d4edda;border-radius:0 4px 4px 0;"><strong>New Time</strong><br>{{.NewTime}}</td>
    </tr>
  </table>
  <p><strong>Timezone:</strong> {{.Timezone}}</p>
  {{ if .ZoomLink }}<p><strong>New Zoom Link:</strong> <a href="{{.ZoomLink}}">{{.ZoomLink}}</a></p>{{ end }}
  <p>If you need to make any further changes, please do so at least 12 hours before the session.</p>
  <p>See you soon!</p>
  <p style="color:#666;font-size:12px;">FitCall Team</p>
</body>
</html>`

const paidRescheduleTrainerTemplate = `<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>Client Rescheduled Training Session</title></head>
<body style="font-family:Arial,sans-serif;max-width:600px;margin:0 auto;padding:20px;">
  <h2 style="color:#1a1a2e;">Client Rescheduled Training Session</h2>
  <p>Your client <strong>{{.ClientName}}</strong> has rescheduled their training session.</p>
  <table style="width:100%;border-collapse:collapse;margin:20px 0;">
    <tr>
      <td style="padding:10px;background:#f8d7da;border-radius:4px 0 0 4px;"><strong>Previous Time</strong><br>{{.OldTime}}</td>
      <td style="padding:10px;background:#d4edda;border-radius:0 4px 4px 0;"><strong>New Time</strong><br>{{.NewTime}}</td>
    </tr>
  </table>
  <p><strong>Timezone:</strong> {{.Timezone}}</p>
  {{ if .ZoomLink }}<p><strong>New Zoom Link:</strong> <a href="{{.ZoomLink}}">{{.ZoomLink}}</a></p>{{ end }}
  <p style="color:#666;font-size:12px;">FitCall Team</p>
</body>
</html>`

const bookingConfirmationSubject = "You've successfully booked a session with us"
const bookingConfirmationTemplate = `<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><title>Session Booked</title></head>
<body style="font-family:Arial,sans-serif;max-width:600px;margin:0 auto;padding:20px;">
  <h2 style="color:#1a1a2e;">
  Hi {{.ClientName}},
  </h2>
  <p>Your FitCall session has been successfully booked</p>
  Session Details:
  <ul>
    <li>Trainer: Coach {{.TrainerName}}.</li>
    <li>Date: {{.Date}}.</li>
    <li>Time: {{.StartTime}} - {{.EndTime}}.</li>
    <li>Location: Zoom.</li>
  </ul>
  <p> Your trainer will check in before the session to help keep you accountable and ready. </p>
  <p>We’re excited to help you stay consistent. </p>

  — Team FitCall

</body>
</html>`

func bookingConfirmation(name, trainerName string, scheduledStartTime, scheduledEndTime time.Time, timezone string, zoomLink string) (string, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	localScheduledStartTime := scheduledStartTime.In(loc)
	localScheduledEndTime := scheduledEndTime.In(loc)

	t, err := template.New("booking-confirmation").Parse(bookingConfirmationTemplate)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]interface{}{
		"ClientName":  name,
		"TrainerName": trainerName,
		"Date":        localScheduledStartTime.Format("Monday, January 2, 2006"),
		"StartTime":   localScheduledStartTime.Format("3:04 PM"),
		"EndTime":     localScheduledEndTime.Format("3:04 PM"),
		"Timezone":    timezone,
		"ZoomLink":    zoomLink,
	})
	return buf.String(), err
}

func (m *SMTPMailer) SendBookingConfirmation(to, clientName, trainerName string, scheduledStartTime, scheduledEndTime time.Time, timezone string, zoomLink string) error {
	html, err := bookingConfirmation(clientName, trainerName, scheduledStartTime, scheduledEndTime, timezone, zoomLink)
	if err != nil {
		return fmt.Errorf("smtp: build paid session reschedule email: %w", err)
	}
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("smtp: invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("smtp: invalid recipient address: %w", err)
	}
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, bookingConfirmationSubject, html,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

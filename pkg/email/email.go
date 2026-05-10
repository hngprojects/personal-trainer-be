package email

import (
	"bytes"
	"fmt"
	"html/template"
	"log/slog"
	"net/mail"
	"net/smtp"
	"strings"
)

type Mailer interface {
	SendVerificationCode(to, code string, expiryMinutes int) error
	SendPasswordResetCode(to, code string, expiryMinutes int) error
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

// LogMailer logs emails instead of sending them — useful in development.
type LogMailer struct{}

func NewLogMailer() *LogMailer { return &LogMailer{} }

func (m *LogMailer) SendVerificationCode(to, code string, expiryMinutes int) error {
	body, err := verificationCodeHTML(code, expiryMinutes)
	if err != nil {
		return err
	}
	slog.Info("email", "to", to, "subject", verificationCodeSubject, "body", body)
	return nil
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

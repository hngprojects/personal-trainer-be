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
	SendAdminCredentials(to, password string) error
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

func (m *LogMailer) SendAdminCredentials(to, password string) error {
	body, err := adminCredentialsHTML(to, password)
	if err != nil {
		return err
	}
	slog.Info("email", "to", to, "subject", adminCredentialsSubject, "body", body)
	return nil
}

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

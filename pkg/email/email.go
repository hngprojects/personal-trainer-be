package email

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

//go:embed templates/*.html
var templates embed.FS

type Mailer interface {
	SendVerificationCode(to, code string, expiryMinutes int) error
	SendAdminCredentials(to, password string) error
	SendTrainerCredentials(to, password string) error
	// SendAccountSetupLink mails a one-time activation link to a newly
	// provisioned account (currently used for trainer onboarding). The
	// trainer never sees a server-generated password — they click the link,
	// land on the FE set-password page, and POST the supplied token to
	// /auth/set-password along with their chosen password.
	SendAccountSetupLink(to, name, link string, expiryHours int) error
	SendPasswordResetCode(to, code string, expiryMinutes int) error
	SendWaitlistConfirmation(to string) error
	SendContactConfirmation(to, name string) error
	SendDiscoveryBookingConfirmation(to, name string, scheduledAt time.Time, timezone, contactMode, phoneNumber, zoomLink string) error
	SendDiscoveryBookingAdminNotification(to, clientName, clientEmail string, scheduledAt time.Time, timezone, contactMode, phoneNumber, zoomLink string) error
	SendDiscoveryRescheduleConfirmation(to, name string, oldTime, newTime time.Time, timezone, contactMode, phoneNumber, zoomLink string) error
	SendPaidSessionRescheduleConfirmation(to, name string, oldTime, newTime time.Time, timezone, zoomLink string) error
	SendPaidSessionRescheduleTrainerNotification(to, clientName string, oldTime, newTime time.Time, timezone, zoomLink string) error
	SendBookingConfirmation(to, clientName, trainerName string, scheduledStartTime, scheduledEndTime time.Time, timezone string, location string, sessionData string, toTrainer bool) error
	SendSessionReminder(to, clientName, trainerName string, scheduledStart time.Time, timezone, zoomLink string) error
	SendSessionReminderTrainer(to, trainerName, clientName string, scheduledStart time.Time, timezone, zoomLink string) error
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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, verificationCodeSubject, body,
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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, adminCredentialsSubject, body,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

// SendAccountSetupLink mails the one-time activation link used by the
// trainer-onboarding flow. Replaces the previous SendTrainerCredentials
// path so the trainer is never sent a server-generated plaintext password.
func (m *SMTPMailer) SendAccountSetupLink(to, name, link string, expiryHours int) error {
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	body, err := accountSetupHTML(name, link, expiryHours)
	if err != nil {
		return fmt.Errorf("build account setup email body: %w", err)
	}
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, accountSetupSubject, body,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

// SendTrainerCredentials mails the generated password to a newly-provisioned
// trainer. Used by POST /trainers (admin-creates-trainer). Mirrors the admin
// credentials flow — same delivery shape, different copy.
func (m *SMTPMailer) SendTrainerCredentials(to, password string) error {
	fromAddr, err := sanitizeAddress(m.from)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	toAddr, err := sanitizeAddress(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	body, err := trainerCredentialsHTML(toAddr, password)
	if err != nil {
		return fmt.Errorf("build trainer credentials email body: %w", err)
	}
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, trainerCredentialsSubject, body,
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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, passwordResetSubject, body,
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

// SendTrainerCredentials logs only metadata — never the rendered body, which
// contains the live plaintext password. Mirrors SendPasswordResetCode's
// redaction policy: if the LogMailer ever runs outside an isolated local
// workflow, anyone with log access could otherwise take over a brand-new
// trainer account. Local E2E flows that need the actual password should use
// a test stub that captures the args, not the LogMailer.
func (m *LogMailer) SendTrainerCredentials(to, _ string) error {
	slog.Info("email (trainer credentials redacted)",
		"to", to,
		"subject", trainerCredentialsSubject,
	)
	return nil
}

// SendAccountSetupLink logs only metadata — never the rendered body or the
// link itself, which contains a live one-time activation token. Same
// reasoning as SendTrainerCredentials: someone reading logs could otherwise
// claim a brand-new account before the legitimate user does.
func (m *LogMailer) SendAccountSetupLink(to, _, _ string, expiryHours int) error {
	slog.Info("email (account setup link redacted)",
		"to", to,
		"subject", accountSetupSubject,
		"expires_in_hours", expiryHours,
	)
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

func (m *LogMailer) SendBookingConfirmation(to, clientName, trainerName string, scheduledStartTime, scheduledEndTime time.Time, timezone string, location string, sessionData string, toTrainer bool) error {
	slog.Info("email (booking confirmation)", "to", to, "client", clientName, "start", scheduledStartTime, "end", scheduledEndTime, "timezone", timezone, "location", location)
	return nil
}

func (m *LogMailer) SendSessionReminder(to, clientName, trainerName string, scheduledStart time.Time, timezone, zoomLink string) error {
	slog.Info("email (session reminder client)", "to", to, "client", clientName, "trainer", trainerName, "start", scheduledStart, "timezone", timezone)
	return nil
}

func (m *LogMailer) SendSessionReminderTrainer(to, trainerName, clientName string, scheduledStart time.Time, timezone, zoomLink string) error {
	slog.Info("email (session reminder trainer)", "to", to, "trainer", trainerName, "client", clientName, "start", scheduledStart, "timezone", timezone)
	return nil
}

const phoneCallConfirmationSubject = "Your FitCall Discovery Call is Confirmed"

var phoneCallConfirmationTemplate, _ = template.ParseFS(templates, "templates/phoneCallConfirmation.html")

const zoomMeetingConfirmationSubject = "Your FitCall Discovery Call is Confirmed"

var zoomMeetingConfirmationTemplate, _ = template.ParseFS(templates, "templates/zoomMeetingConfirmation.html")

const discoveryBookingAdminNotificationSubject = "New Discovery Call Booking"

var discoveryBookingAdminTemplate, _ = template.ParseFS(templates, "templates/discoveryBookingAdmin.html")

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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, subject, body,
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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, discoveryBookingAdminNotificationSubject, body,
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

var verificationCodeTemplate, _ = template.ParseFS(templates, "templates/verificationCode.html")

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

var adminCredentialsTemplate, _ = template.ParseFS(templates, "templates/adminCredentials.html")

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

const trainerCredentialsSubject = "Your FitCall trainer account is ready"

var trainerCredentialsTemplate, _ = template.ParseFS(templates, "templates/trainerCredentials.html")

func trainerCredentialsHTML(emailAddr, password string) (string, error) {
	var body bytes.Buffer
	if err := trainerCredentialsTemplate.Execute(&body, struct {
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

const accountSetupSubject = "Set up your FitCall account"

// accountSetupTemplate renders the one-time activation link. The link
// embeds the token in the query string; the FE set-password page extracts
// it and POSTs it to /auth/set-password with the chosen new password.
//
// We deliberately don't include the email address in the link — the token
// is the only identifier the consume endpoint uses, and omitting the
// email reduces the shoulder-surfing surface (recipient already knows
// their own email; the link doesn't need to advertise it).
var accountSetupTemplate, _ = template.ParseFS(templates, "templates/accountSetup.html")

func accountSetupHTML(name, link string, expiryHours int) (string, error) {
	var body bytes.Buffer
	if err := accountSetupTemplate.Execute(&body, struct {
		Name        string
		Link        string
		ExpiryHours int
	}{
		Name:        name,
		Link:        link,
		ExpiryHours: expiryHours,
	}); err != nil {
		return "", err
	}
	return body.String(), nil
}

const passwordResetSubject = "Your password reset code"

var passwordResetTemplate, _ = template.ParseFS(templates, "templates/passwordReset.html")

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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, waitlistConfirmationSubject, body,
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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, contactConfirmationSubject, body,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

const waitlistConfirmationSubject = "You're on the waitlist!"

var waitlistConfirmationTemplate, _ = template.ParseFS(templates, "templates/waitlistConfirmation.html")

func waitlistConfirmationHTML() (string, error) {
	var body bytes.Buffer
	if err := waitlistConfirmationTemplate.Execute(&body, nil); err != nil {
		return "", err
	}
	return body.String(), nil
}

const contactConfirmationSubject = "We received your message!"

var contactConfirmationTemplate, _ = template.ParseFS(templates, "templates/contactConfirmation.html")

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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, discoveryRescheduleSubject, html,
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

	var buf bytes.Buffer
	err = discoveryRescheduleTemplate.Execute(&buf, map[string]interface{}{
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

var discoveryRescheduleTemplate, _ = template.ParseFS(templates, "templates/discoveryReschedule.html")

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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, paidRescheduleClientSubject, html,
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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, paidRescheduleTrainerSubject, html,
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

	var buf bytes.Buffer
	err = paidRescheduleClientTemplate.Execute(&buf, map[string]interface{}{
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

	var buf bytes.Buffer
	err = paidRescheduleTrainerTemplate.Execute(&buf, map[string]interface{}{
		"ClientName": clientName,
		"OldTime":    oldTime.In(loc).Format("Monday, January 2, 2006 at 3:04 PM"),
		"NewTime":    newTime.In(loc).Format("Monday, January 2, 2006 at 3:04 PM"),
		"Timezone":   timezone,
		"ZoomLink":   zoomLink,
	})
	return buf.String(), err
}

var paidRescheduleClientTemplate, _ = template.ParseFS(templates, "templates/paidReschedule.html")

var paidRescheduleTrainerTemplate, _ = template.ParseFS(templates, "templates/paidRescheduleTrainer.html")

const bookingConfirmationSubject = "You've successfully booked a session with us"

var bookingConfirmationTemplate, _ = template.ParseFS(templates, "templates/bookingConfirmation.html")

var trainerBookingConfirmationTemplate, _ = template.ParseFS(templates, "templates/trainerBookingConfirmation.html")

func bookingConfirmation(name, trainerName string, scheduledStartTime, scheduledEndTime time.Time, timezone string, location string, sessionData string, toTrainer bool) (string, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
	}
	localScheduledStartTime := scheduledStartTime.In(loc)
	localScheduledEndTime := scheduledEndTime.In(loc)

	var buf bytes.Buffer
	if toTrainer {
		err = trainerBookingConfirmationTemplate.Execute(&buf, map[string]interface{}{
			"ClientName":  name,
			"TrainerName": trainerName,
			"Date":        localScheduledStartTime.Format("Monday, January 2, 2006"),
			"StartTime":   localScheduledStartTime.Format("3:04 PM"),
			"EndTime":     localScheduledEndTime.Format("3:04 PM"),
			"Timezone":    timezone,
			"Location":    location,
			"SessionData": sessionData,
		})
		return buf.String(), err
	}
	err = bookingConfirmationTemplate.Execute(&buf, map[string]interface{}{
		"ClientName":  name,
		"TrainerName": trainerName,
		"Date":        localScheduledStartTime.Format("Monday, January 2, 2006"),
		"StartTime":   localScheduledStartTime.Format("3:04 PM"),
		"EndTime":     localScheduledEndTime.Format("3:04 PM"),
		"Timezone":    timezone,
		"Location":    location,
		"SessionData": sessionData,
	})
	return buf.String(), err

}

func (m *SMTPMailer) SendBookingConfirmation(to, clientName, trainerName string, scheduledStartTime, scheduledEndTime time.Time, timezone string, location string, sessionData string, toTrainer bool) error {
	html, err := bookingConfirmation(clientName, trainerName, scheduledStartTime, scheduledEndTime, timezone, location, sessionData, toTrainer)
	if err != nil {
		return fmt.Errorf("smtp: build booking confirmation email: %w", err)
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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, bookingConfirmationSubject, html,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

const (
	sessionReminderClientSubject  = "Your FitCall Session Starts in 1 Hour"
	sessionReminderTrainerSubject = "Upcoming FitCall Session in 1 Hour"
)

var sessionReminderClientTemplate, _ = template.ParseFS(templates, "templates/sessionReminder.html")
var sessionReminderTrainerTemplate, _ = template.ParseFS(templates, "templates/sessionReminderTrainer.html")

func sessionReminderClientHTML(clientName, trainerName string, scheduledStart time.Time, timezone, zoomLink string) (string, error) {
	timezoneLabel := timezone
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
		timezoneLabel = "UTC"
	}

	var buf bytes.Buffer
	err = sessionReminderClientTemplate.Execute(&buf, map[string]interface{}{
		"ClientName":  clientName,
		"TrainerName": trainerName,
		"Time":        scheduledStart.In(loc).Format("3:04 PM"),
		"Timezone":    timezoneLabel,
		"ZoomLink":    zoomLink,
	})
	return buf.String(), err
}

func sessionReminderTrainerHTML(trainerName, clientName string, scheduledStart time.Time, timezone, zoomLink string) (string, error) {
	timezoneLabel := timezone
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		loc = time.UTC
		timezoneLabel = "UTC"
	}

	var buf bytes.Buffer
	err = sessionReminderTrainerTemplate.Execute(&buf, map[string]interface{}{
		"TrainerName": trainerName,
		"ClientName":  clientName,
		"Time":        scheduledStart.In(loc).Format("3:04 PM"),
		"Timezone":    timezoneLabel,
		"ZoomLink":    zoomLink,
	})
	return buf.String(), err
}

func (m *SMTPMailer) SendSessionReminder(to, clientName, trainerName string, scheduledStart time.Time, timezone, zoomLink string) error {
	html, err := sessionReminderClientHTML(clientName, trainerName, scheduledStart, timezone, zoomLink)
	if err != nil {
		return fmt.Errorf("smtp: build session reminder email: %w", err)
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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, sessionReminderClientSubject, html,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

func (m *SMTPMailer) SendSessionReminderTrainer(to, trainerName, clientName string, scheduledStart time.Time, timezone, zoomLink string) error {
	html, err := sessionReminderTrainerHTML(trainerName, clientName, scheduledStart, timezone, zoomLink)
	if err != nil {
		return fmt.Errorf("smtp: build session reminder trainer email: %w", err)
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
		"From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n%s",
		fromAddr, toAddr, sessionReminderTrainerSubject, html,
	)
	return smtp.SendMail(m.host+":"+m.port, auth, fromAddr, []string{toAddr}, []byte(msg))
}

package email

import (
	"fmt"
	"log/slog"
	"net/smtp"
)

type Mailer interface {
	Send(to, subject, body string) error
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

func (m *SMTPMailer) Send(to, subject, body string) error {
	auth := smtp.PlainAuth("", m.username, m.password, m.host)
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s", m.from, to, subject, body)
	return smtp.SendMail(m.host+":"+m.port, auth, m.from, []string{to}, []byte(msg))
}

// LogMailer logs emails instead of sending them — useful in development.
type LogMailer struct{}

func NewLogMailer() *LogMailer { return &LogMailer{} }

func (m *LogMailer) Send(to, subject, body string) error {
	slog.Info("email", "to", to, "subject", subject, "body", body)
	return nil
}

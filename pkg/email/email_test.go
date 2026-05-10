package email

import "testing"

func TestSanitizeHeaderValueRejectsNewlines(t *testing.T) {
	t.Parallel()

	if _, err := sanitizeHeaderValue("hello\r\nBcc: attacker@example.com"); err == nil {
		t.Fatal("expected newline-containing header value to be rejected")
	}
}

func TestSMTPMailerSendRejectsInjectedRecipient(t *testing.T) {
	t.Parallel()

	mailer := NewSMTPMailer("smtp.example.com", "587", "user", "pass", "from@example.com")

	err := mailer.SendVerificationCode("victim@example.com\r\nBcc: attacker@example.com", "123456", 15)
	if err == nil {
		t.Fatal("expected injected recipient to be rejected")
	}
}

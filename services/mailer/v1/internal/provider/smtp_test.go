package provider

import (
	"fmt"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	cordismail "github.com/soasurs/cordis/pkg/mail"
)

func TestSMTPSend(t *testing.T) {
	tests := []struct {
		name        string
		template    string
		expectedURL string
	}{
		{
			name:        "email verification",
			template:    cordismail.TemplateEmailVerification,
			expectedURL: "http://localhost:5173/verify-email?token=" + url.QueryEscape("secret/token+value"),
		},
		{
			name:        "password reset preserves query",
			template:    cordismail.TemplatePasswordReset,
			expectedURL: "http://localhost:5173/reset-password?source=mail&token=" + url.QueryEscape("secret/token+value"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			address, deliveries, serverErr := startSMTPServer(t)
			provider, err := NewSMTP(SMTPConfig{
				Address:              address,
				From:                 "Cordis <no-reply@cordis.localhost>",
				Timeout:              time.Second,
				PasswordResetURL:     "http://localhost:5173/reset-password?source=mail",
				EmailVerificationURL: "http://localhost:5173/verify-email",
			})
			require.NoError(t, err)

			err = provider.Send(t.Context(), "user@example.com", tt.template, map[string]string{
				cordismail.VariableToken: "secret/token+value",
			})
			require.NoError(t, err)
			require.NoError(t, <-serverErr)

			delivery := <-deliveries
			require.Equal(t, "no-reply@cordis.localhost", delivery.from)
			require.Equal(t, "user@example.com", delivery.to)
			require.Contains(t, delivery.message, "From: \"Cordis\" <no-reply@cordis.localhost>\n")
			require.Contains(t, delivery.message, "To: <user@example.com>\n")
			require.Contains(t, delivery.message, tt.expectedURL)
		})
	}
}

func TestSMTPRequiresTLS(t *testing.T) {
	address, _, serverErr := startSMTPServer(t)
	provider, err := NewSMTP(SMTPConfig{
		Address:              address,
		From:                 "no-reply@cordis.localhost",
		RequireTLS:           true,
		Timeout:              time.Second,
		PasswordResetURL:     "http://localhost:5173/reset-password",
		EmailVerificationURL: "http://localhost:5173/verify-email",
	})
	require.NoError(t, err)

	err = provider.Send(t.Context(), "user@example.com", cordismail.TemplateEmailVerification, map[string]string{
		cordismail.VariableToken: "secret",
	})
	require.EqualError(t, err, "smtp server does not support STARTTLS")
	require.NoError(t, <-serverErr)
}

func TestNewSMTPValidation(t *testing.T) {
	valid := SMTPConfig{
		Address:              "mailpit:1025",
		From:                 "Cordis <no-reply@cordis.localhost>",
		Timeout:              time.Second,
		PasswordResetURL:     "https://app.example.com/reset-password",
		EmailVerificationURL: "https://app.example.com/verify-email",
	}

	tests := []struct {
		name   string
		mutate func(*SMTPConfig)
	}{
		{name: "address", mutate: func(cfg *SMTPConfig) { cfg.Address = "mailpit" }},
		{name: "from", mutate: func(cfg *SMTPConfig) { cfg.From = "invalid" }},
		{name: "partial auth", mutate: func(cfg *SMTPConfig) { cfg.Username = "user" }},
		{name: "timeout", mutate: func(cfg *SMTPConfig) { cfg.Timeout = 0 }},
		{name: "password reset url", mutate: func(cfg *SMTPConfig) { cfg.PasswordResetURL = "ftp://example.com/reset" }},
		{name: "verification url", mutate: func(cfg *SMTPConfig) { cfg.EmailVerificationURL = "https://user@example.com/verify" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)
			_, err := NewSMTP(cfg)
			require.Error(t, err)
		})
	}
}

func TestSMTPRejectsInvalidTemplateInputBeforeDial(t *testing.T) {
	provider, err := NewSMTP(SMTPConfig{
		Address:              "127.0.0.1:1",
		From:                 "no-reply@cordis.localhost",
		Timeout:              time.Second,
		PasswordResetURL:     "http://localhost:5173/reset-password",
		EmailVerificationURL: "http://localhost:5173/verify-email",
	})
	require.NoError(t, err)

	err = provider.Send(t.Context(), "user@example.com", cordismail.TemplateEmailVerification, nil)
	require.EqualError(t, err, "template token is required")
	err = provider.Send(t.Context(), "user@example.com", "unknown", map[string]string{cordismail.VariableToken: "secret"})
	require.EqualError(t, err, "unknown email template")
}

type smtpDelivery struct {
	from    string
	to      string
	message string
}

func startSMTPServer(t *testing.T) (string, <-chan smtpDelivery, <-chan error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	deliveries := make(chan smtpDelivery, 1)
	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer func() { _ = conn.Close() }()
		serverErr <- serveSMTP(conn, deliveries)
	}()
	return listener.Addr().String(), deliveries, serverErr
}

func serveSMTP(conn net.Conn, deliveries chan<- smtpDelivery) error {
	text := textproto.NewConn(conn)
	defer func() { _ = text.Close() }()
	if err := text.PrintfLine("220 localhost ESMTP"); err != nil {
		return err
	}

	var delivery smtpDelivery
	for {
		line, err := text.ReadLine()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		command := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(command, "EHLO "):
			if err := text.PrintfLine("250-localhost"); err != nil {
				return err
			}
			if err := text.PrintfLine("250 HELP"); err != nil {
				return err
			}
		case strings.HasPrefix(command, "HELO "):
			if err := text.PrintfLine("250 localhost"); err != nil {
				return err
			}
		case strings.HasPrefix(command, "MAIL FROM:"):
			delivery.from = smtpPath(line)
			if err := text.PrintfLine("250 OK"); err != nil {
				return err
			}
		case strings.HasPrefix(command, "RCPT TO:"):
			delivery.to = smtpPath(line)
			if err := text.PrintfLine("250 OK"); err != nil {
				return err
			}
		case command == "DATA":
			if err := text.PrintfLine("354 End data with <CR><LF>.<CR><LF>"); err != nil {
				return err
			}
			message, err := io.ReadAll(text.DotReader())
			if err != nil {
				return err
			}
			delivery.message = string(message)
			deliveries <- delivery
			if err := text.PrintfLine("250 queued"); err != nil {
				return err
			}
		case command == "QUIT":
			if err := text.PrintfLine("221 bye"); err != nil {
				return err
			}
			return nil
		default:
			return fmt.Errorf("unexpected smtp command %q", line)
		}
	}
}

func smtpPath(line string) string {
	start := strings.IndexByte(line, '<')
	end := strings.IndexByte(line, '>')
	if start == -1 || end <= start {
		return ""
	}
	return line[start+1 : end]
}

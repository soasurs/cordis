package provider

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	stdmail "net/mail"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	cordismail "github.com/soasurs/cordis/pkg/mail"
)

// SMTPConfig configures SMTP delivery and the public links rendered into
// transactional emails.
type SMTPConfig struct {
	Address              string
	From                 string
	Username             string
	Password             string
	RequireTLS           bool
	Timeout              time.Duration
	PasswordResetURL     string
	EmailVerificationURL string
}

// SMTP delivers rendered transactional emails over SMTP.
type SMTP struct {
	address              string
	host                 string
	from                 *stdmail.Address
	username             string
	password             string
	requireTLS           bool
	timeout              time.Duration
	passwordResetURL     *url.URL
	emailVerificationURL *url.URL
}

// NewSMTP validates config without making a network connection.
func NewSMTP(cfg SMTPConfig) (*SMTP, error) {
	address := strings.TrimSpace(cfg.Address)
	host, _, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return nil, errors.New("smtp address must use host:port")
	}
	from, err := stdmail.ParseAddress(strings.TrimSpace(cfg.From))
	if err != nil || from.Address == "" {
		return nil, errors.New("smtp from address is invalid")
	}
	if (cfg.Username == "") != (cfg.Password == "") {
		return nil, errors.New("smtp username and password must be set together")
	}
	if cfg.Timeout <= 0 {
		return nil, errors.New("smtp timeout must be positive")
	}
	passwordResetURL, err := parseTemplateURL("password reset", cfg.PasswordResetURL)
	if err != nil {
		return nil, err
	}
	emailVerificationURL, err := parseTemplateURL("email verification", cfg.EmailVerificationURL)
	if err != nil {
		return nil, err
	}

	return &SMTP{
		address:              address,
		host:                 host,
		from:                 from,
		username:             cfg.Username,
		password:             cfg.Password,
		requireTLS:           cfg.RequireTLS,
		timeout:              cfg.Timeout,
		passwordResetURL:     passwordResetURL,
		emailVerificationURL: emailVerificationURL,
	}, nil
}

// Send renders a known template and delivers it to one recipient.
func (p *SMTP) Send(ctx context.Context, to, template string, variables map[string]string) error {
	recipient, err := stdmail.ParseAddress(strings.TrimSpace(to))
	if err != nil || recipient.Address == "" {
		return errors.New("smtp recipient is invalid")
	}
	message, err := p.render(recipient, template, variables)
	if err != nil {
		return err
	}

	dialer := new(net.Dialer)
	conn, err := dialer.DialContext(ctx, "tcp", p.address)
	if err != nil {
		return fmt.Errorf("dial smtp: %w", err)
	}
	deadline := time.Now().Add(p.timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		_ = conn.Close()
		return fmt.Errorf("set smtp deadline: %w", err)
	}

	client, err := smtp.NewClient(conn, p.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("create smtp client: %w", err)
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: p.host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("start smtp tls: %w", err)
		}
	} else if p.requireTLS {
		return errors.New("smtp server does not support STARTTLS")
	}
	if p.username != "" {
		if err := client.Auth(smtp.PlainAuth("", p.username, p.password, p.host)); err != nil {
			return fmt.Errorf("authenticate smtp: %w", err)
		}
	}
	if err := client.Mail(p.from.Address); err != nil {
		return fmt.Errorf("set smtp sender: %w", err)
	}
	if err := client.Rcpt(recipient.Address); err != nil {
		return fmt.Errorf("set smtp recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("start smtp data: %w", err)
	}
	if _, err := writer.Write(message); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write smtp data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish smtp data: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("quit smtp: %w", err)
	}
	return nil
}

func (p *SMTP) render(recipient *stdmail.Address, template string, variables map[string]string) ([]byte, error) {
	token := strings.TrimSpace(variables[cordismail.VariableToken])
	if token == "" {
		return nil, errors.New("template token is required")
	}

	var subject, introduction string
	var templateURL *url.URL
	switch template {
	case cordismail.TemplateEmailVerification:
		subject = "Verify your Cordis email"
		introduction = "Verify your email address to finish setting up your Cordis account:"
		templateURL = p.emailVerificationURL
	case cordismail.TemplatePasswordReset:
		subject = "Reset your Cordis password"
		introduction = "Use this link to reset your Cordis password:"
		templateURL = p.passwordResetURL
	default:
		return nil, errors.New("unknown email template")
	}

	link := *templateURL
	query := link.Query()
	query.Set("token", token)
	link.RawQuery = query.Encode()
	body := introduction + "\r\n\r\n" + link.String() + "\r\n\r\n" +
		"If you did not request this email, you can ignore it.\r\n"

	var message strings.Builder
	message.WriteString("From: ")
	message.WriteString(p.from.String())
	message.WriteString("\r\nTo: ")
	message.WriteString(recipient.String())
	message.WriteString("\r\nSubject: ")
	message.WriteString(subject)
	message.WriteString("\r\nMIME-Version: 1.0")
	message.WriteString("\r\nContent-Type: text/plain; charset=UTF-8")
	message.WriteString("\r\nContent-Transfer-Encoding: 8bit")
	message.WriteString("\r\n\r\n")
	message.WriteString(body)
	return []byte(message.String()), nil
}

func parseTemplateURL(name, rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, fmt.Errorf("%s url must be an absolute http or https URL without credentials or fragment", name)
	}
	return parsed, nil
}

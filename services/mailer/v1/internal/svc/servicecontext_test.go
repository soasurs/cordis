package svc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/mailer/v1/config"
	"github.com/soasurs/cordis/services/mailer/v1/internal/provider"
)

func TestNewDependenciesSelectsProvider(t *testing.T) {
	noopDeps, err := NewDependencies(config.Config{})
	require.NoError(t, err)
	require.IsType(t, new(provider.Noop), noopDeps.Provider)

	smtpDeps, err := NewDependencies(config.Config{Mailer: config.MailerConfig{
		Provider:             "smtp",
		From:                 "Cordis <no-reply@cordis.localhost>",
		PasswordResetURL:     "http://localhost:5173/reset-password",
		EmailVerificationURL: "http://localhost:5173/verify-email",
		SMTP: config.SMTPConfig{
			Address: "mailpit:1025",
			Timeout: time.Second,
		},
	}})
	require.NoError(t, err)
	require.IsType(t, new(provider.SMTP), smtpDeps.Provider)
}

func TestNewDependenciesRejectsInvalidProviderConfig(t *testing.T) {
	_, err := NewDependencies(config.Config{Mailer: config.MailerConfig{Provider: "smtp"}})
	require.Error(t, err)

	_, err = NewDependencies(config.Config{Mailer: config.MailerConfig{Provider: "unsupported"}})
	require.EqualError(t, err, "unsupported mailer provider")
}

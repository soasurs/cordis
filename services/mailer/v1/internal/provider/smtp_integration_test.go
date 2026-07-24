//go:build integration

package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	cordismail "github.com/soasurs/cordis/pkg/mail"
)

const mailpitImage = "axllent/mailpit:v1.30.5"

func TestSMTPWithMailpit(t *testing.T) {
	container, err := testcontainers.Run(t.Context(), mailpitImage,
		testcontainers.WithExposedPorts("1025/tcp", "8025/tcp"),
		testcontainers.WithWaitStrategy(wait.ForHTTP("/readyz").WithPort("8025/tcp").WithStartupTimeout(30*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		require.NoError(t, container.Terminate(ctx))
	})

	smtpAddress, err := container.PortEndpoint(t.Context(), "1025/tcp", "")
	require.NoError(t, err)
	httpEndpoint, err := container.PortEndpoint(t.Context(), "8025/tcp", "http")
	require.NoError(t, err)

	provider, err := NewSMTP(SMTPConfig{
		Address:              smtpAddress,
		From:                 "Cordis <no-reply@cordis.localhost>",
		Timeout:              5 * time.Second,
		PasswordResetURL:     "http://localhost:5173/reset-password",
		EmailVerificationURL: "http://localhost:5173/verify-email",
	})
	require.NoError(t, err)
	require.NoError(t, provider.Send(t.Context(), "user@example.com", cordismail.TemplateEmailVerification, map[string]string{
		cordismail.VariableToken: "integration-secret-token",
	}))

	client := &http.Client{Timeout: 5 * time.Second}
	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, httpEndpoint+"/api/v1/messages", nil)
		assert.NoError(collect, err)
		response, err := client.Do(request)
		if !assert.NoError(collect, err) {
			return
		}
		defer func() { _ = response.Body.Close() }()
		assert.Equal(collect, http.StatusOK, response.StatusCode)
		var messages struct {
			Messages []struct {
				Subject string `json:"Subject"`
			} `json:"messages"`
		}
		assert.NoError(collect, json.NewDecoder(response.Body).Decode(&messages))
		if assert.Len(collect, messages.Messages, 1) {
			assert.Equal(collect, "Verify your Cordis email", messages.Messages[0].Subject)
		}
	}, 5*time.Second, 100*time.Millisecond)
}

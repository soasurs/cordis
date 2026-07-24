package svc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/authenticator/v1/config"
)

func TestValidateRegistrationConfig(t *testing.T) {
	for _, mode := range []string{
		"",
		config.RegistrationModeOpen,
		config.RegistrationModeInviteOnly,
		config.RegistrationModeClosed,
	} {
		require.NoError(t, validateRegistrationConfig(config.RegistrationConfig{Mode: mode}))
	}
	require.Error(t, validateRegistrationConfig(config.RegistrationConfig{Mode: "unknown"}))
	require.Error(t, validateRegistrationConfig(config.RegistrationConfig{
		Mode: config.RegistrationModeInviteOnly, ReservationTTL: -time.Second,
	}))
}

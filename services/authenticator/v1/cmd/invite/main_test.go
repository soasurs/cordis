package main

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
)

func TestNormalizeBoundEmail(t *testing.T) {
	email, err := normalizeBoundEmail(" User@Example.COM ")
	require.NoError(t, err)
	require.Equal(t, "user@example.com", email)

	_, err = normalizeBoundEmail("not-an-email")
	require.Error(t, err)
}

func TestInviteStatus(t *testing.T) {
	const now = int64(1000)
	tests := []struct {
		name   string
		invite *model.RegistrationInvite
		want   string
	}{
		{name: "available", invite: new(model.RegistrationInvite), want: "available"},
		{name: "reserved", invite: &model.RegistrationInvite{ReservedUntil: now + 1}, want: "reserved"},
		{name: "expired", invite: &model.RegistrationInvite{ExpiresAt: now}, want: "expired"},
		{name: "revoked", invite: &model.RegistrationInvite{RevokedAt: now}, want: "revoked"},
		{name: "redeemed", invite: &model.RegistrationInvite{RedeemedAt: now}, want: "redeemed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, inviteStatus(tc.invite, now))
		})
	}
}

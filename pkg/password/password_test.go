package password

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHashReturnsArgon2IDPHCString(t *testing.T) {
	hashedPassword, err := Hash("correct horse battery staple")
	require.NoError(t, err)

	const prefix = "$argon2id$v=19$m=19456,t=2,p=1$"
	require.True(t, strings.HasPrefix(hashedPassword, prefix), "Hash() = %q, want prefix %q", hashedPassword, prefix)

	parts := strings.Split(hashedPassword, "$")
	require.Len(t, parts, 6)

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	require.NoError(t, err)
	require.Len(t, salt, saltLength)

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	require.NoError(t, err)
	require.Len(t, hash, keyLength)
}

func TestHashUsesRandomSalt(t *testing.T) {
	first, err := Hash("same password")
	require.NoError(t, err)

	second, err := Hash("same password")
	require.NoError(t, err)

	require.NotEqual(t, first, second, "Hash() returned identical values for the same password")
}

func TestVerifyAcceptsCorrectPassword(t *testing.T) {
	hashedPassword, err := Hash("correct horse battery staple")
	require.NoError(t, err)

	ok, err := Verify(hashedPassword, "correct horse battery staple")
	require.NoError(t, err)
	require.True(t, ok)
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	hashedPassword, err := Hash("correct horse battery staple")
	require.NoError(t, err)

	ok, err := Verify(hashedPassword, "wrong password")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestVerifyRejectsInvalidHash(t *testing.T) {
	ok, err := Verify("not-a-password-hash", "password")
	require.Error(t, err)
	require.False(t, ok)
}

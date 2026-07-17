package twofactor

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCipherBindsSecretToUser(t *testing.T) {
	cipher, err := NewCipher("current", []KeyConfig{{
		ID:     "current",
		Secret: base64.StdEncoding.EncodeToString(make([]byte, 32)),
	}})
	require.NoError(t, err)

	value, err := cipher.Encrypt(1001, []byte("secret"))
	require.NoError(t, err)

	secret, err := cipher.Decrypt(1001, value)
	require.NoError(t, err)
	require.Equal(t, []byte("secret"), secret)

	_, err = cipher.Decrypt(1002, value)
	require.ErrorIs(t, err, ErrDecrypt)
}

func TestVerifyCodeRejectsReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1_700_000_000, 0)
	counter := now.Unix() / period
	code := codeForCounter(secret, counter)

	usedCounter, err := VerifyCode(secret, code, now, counter-1)
	require.NoError(t, err)
	require.Equal(t, counter, usedCounter)

	_, err = VerifyCode(secret, code, now, usedCounter)
	require.ErrorIs(t, err, ErrInvalidCode)
}

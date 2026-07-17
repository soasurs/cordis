package twofactor

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	secretLength = 20
	codeDigits   = 6
	period       = 30
)

var (
	ErrInvalidCode = errors.New("invalid two-factor code")
	ErrDecrypt     = errors.New("decrypt two-factor secret")
)

type KeyConfig struct {
	ID     string
	Secret string
}

type Ciphertext struct {
	KeyID string
	Data  []byte
}

type Cipher struct {
	primaryKeyID string
	keys         map[string]cipher.AEAD
}

func NewCipher(primaryKeyID string, keyConfigs []KeyConfig) (*Cipher, error) {
	if primaryKeyID == "" {
		return nil, errors.New("two-factor primary encryption key id is required")
	}

	keys := make(map[string]cipher.AEAD, len(keyConfigs))
	for _, keyConfig := range keyConfigs {
		if keyConfig.ID == "" {
			return nil, errors.New("two-factor encryption key id is required")
		}
		if _, ok := keys[keyConfig.ID]; ok {
			return nil, fmt.Errorf("duplicate two-factor encryption key id %q", keyConfig.ID)
		}

		key, err := base64.StdEncoding.DecodeString(keyConfig.Secret)
		if err != nil {
			return nil, fmt.Errorf("decode two-factor encryption key %q: %w", keyConfig.ID, err)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("two-factor encryption key %q must contain 32 bytes", keyConfig.ID)
		}
		block, err := aes.NewCipher(key)
		if err != nil {
			return nil, fmt.Errorf("create two-factor cipher %q: %w", keyConfig.ID, err)
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, fmt.Errorf("create two-factor GCM %q: %w", keyConfig.ID, err)
		}
		keys[keyConfig.ID] = aead
	}
	if _, ok := keys[primaryKeyID]; !ok {
		return nil, fmt.Errorf("two-factor primary encryption key %q is not configured", primaryKeyID)
	}

	return &Cipher{primaryKeyID: primaryKeyID, keys: keys}, nil
}

func (c *Cipher) Encrypt(userID int64, secret []byte) (Ciphertext, error) {
	aead := c.keys[c.primaryKeyID]
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Ciphertext{}, fmt.Errorf("generate two-factor nonce: %w", err)
	}

	data := append(nonce, aead.Seal(nil, nonce, secret, associatedData(userID))...)
	return Ciphertext{KeyID: c.primaryKeyID, Data: data}, nil
}

func (c *Cipher) Decrypt(userID int64, value Ciphertext) ([]byte, error) {
	aead, ok := c.keys[value.KeyID]
	if !ok || len(value.Data) < aeadNonceSize(aead) {
		return nil, ErrDecrypt
	}
	nonceSize := aead.NonceSize()
	secret, err := aead.Open(nil, value.Data[:nonceSize], value.Data[nonceSize:], associatedData(userID))
	if err != nil {
		return nil, ErrDecrypt
	}
	return secret, nil
}

func GenerateSecret() ([]byte, string, error) {
	secret := make([]byte, secretLength)
	if _, err := rand.Read(secret); err != nil {
		return nil, "", fmt.Errorf("generate TOTP secret: %w", err)
	}
	return secret, base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret), nil
}

func OTPAuthURI(issuer, accountName, manualEntryKey string) string {
	label := url.PathEscape(issuer + ":" + accountName)
	values := url.Values{}
	values.Set("secret", manualEntryKey)
	values.Set("issuer", issuer)
	values.Set("algorithm", "SHA1")
	values.Set("digits", strconv.Itoa(codeDigits))
	values.Set("period", strconv.Itoa(period))
	return "otpauth://totp/" + label + "?" + values.Encode()
}

func VerifyCode(secret []byte, rawCode string, now time.Time, lastUsedCounter int64) (int64, error) {
	code := strings.TrimSpace(rawCode)
	if len(code) != codeDigits {
		return 0, ErrInvalidCode
	}
	for _, char := range code {
		if char < '0' || char > '9' {
			return 0, ErrInvalidCode
		}
	}

	currentCounter := now.Unix() / period
	for counter := currentCounter - 1; counter <= currentCounter+1; counter++ {
		if counter <= lastUsedCounter {
			continue
		}
		expected := codeForCounter(secret, counter)
		if hmac.Equal([]byte(code), []byte(expected)) {
			return counter, nil
		}
	}
	return 0, ErrInvalidCode
}

func GenerateRecoveryCode() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate recovery code: %w", err)
	}
	encoded := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(value)
	return encoded[:5] + "-" + encoded[5:10] + "-" + encoded[10:15] + "-" + encoded[15:], nil
}

func NormalizeRecoveryCode(raw string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(raw), "-", ""))
}

func associatedData(userID int64) []byte {
	return []byte("cordis.authenticator.v1/totp/user/" + strconv.FormatInt(userID, 10))
}

func codeForCounter(secret []byte, counter int64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(counter))
	mac := hmac.New(sha1.New, secret)
	_, _ = mac.Write(buf[:])
	digest := mac.Sum(nil)
	offset := int(digest[len(digest)-1] & 0x0f)
	value := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1_000_000)
}

func aeadNonceSize(aead cipher.AEAD) int {
	if aead == nil {
		return 0
	}
	return aead.NonceSize()
}

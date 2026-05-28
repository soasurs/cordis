package password

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestHashReturnsArgon2IDPHCString(t *testing.T) {
	hashedPassword, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	const prefix = "$argon2id$v=19$m=19456,t=2,p=1$"
	if !strings.HasPrefix(hashedPassword, prefix) {
		t.Fatalf("Hash() = %q, want prefix %q", hashedPassword, prefix)
	}

	parts := strings.Split(hashedPassword, "$")
	if len(parts) != 6 {
		t.Fatalf("Hash() split into %d parts, want 6: %q", len(parts), hashedPassword)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		t.Fatalf("decode salt: %v", err)
	}
	if len(salt) != saltLength {
		t.Fatalf("salt length = %d, want %d", len(salt), saltLength)
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		t.Fatalf("decode hash: %v", err)
	}
	if len(hash) != keyLength {
		t.Fatalf("hash length = %d, want %d", len(hash), keyLength)
	}
}

func TestHashUsesRandomSalt(t *testing.T) {
	first, err := Hash("same password")
	if err != nil {
		t.Fatalf("Hash() first error = %v", err)
	}

	second, err := Hash("same password")
	if err != nil {
		t.Fatalf("Hash() second error = %v", err)
	}

	if first == second {
		t.Fatal("Hash() returned identical values for the same password")
	}
}

func TestVerifyAcceptsCorrectPassword(t *testing.T) {
	hashedPassword, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	ok, err := Verify(hashedPassword, "correct horse battery staple")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !ok {
		t.Fatal("Verify() = false, want true")
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	hashedPassword, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash() error = %v", err)
	}

	ok, err := Verify(hashedPassword, "wrong password")
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if ok {
		t.Fatal("Verify() = true, want false")
	}
}

func TestVerifyRejectsInvalidHash(t *testing.T) {
	ok, err := Verify("not-a-password-hash", "password")
	if err == nil {
		t.Fatal("Verify() error = nil, want error")
	}
	if ok {
		t.Fatal("Verify() = true, want false")
	}
}

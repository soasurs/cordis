package model

type TOTPFactor struct {
	UserID           int64
	SecretCiphertext []byte
	EncryptionKeyID  string
	LastUsedCounter  int64
	EnabledAt        int64
	CreatedAt        int64
	UpdatedAt        int64
}

type TOTPEnrollment struct {
	UserID           int64
	TokenHash        string
	SecretCiphertext []byte
	EncryptionKeyID  string
	CreatedAt        int64
	ExpiresAt        int64
}

type TwoFactorLoginChallenge struct {
	TokenHash  string
	UserID     int64
	UserAgent  string
	IP         string
	Attempts   int
	CreatedAt  int64
	ExpiresAt  int64
	ConsumedAt int64
}

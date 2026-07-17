package model

type PasswordResetToken struct {
	UserID     int64
	TokenHash  string
	CreatedAt  int64
	ExpiresAt  int64
	ConsumedAt int64
}

type EmailVerificationToken struct {
	UserID     int64
	TokenHash  string
	Email      string
	CreatedAt  int64
	ExpiresAt  int64
	ConsumedAt int64
}

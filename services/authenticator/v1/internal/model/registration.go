package model

// RegistrationInvite is a one-time authorization to create an account.
type RegistrationInvite struct {
	ID             int64
	CodeHash       string
	BoundEmail     string
	ReservedEmail  string
	ReservedUntil  int64
	RedeemedUserID int64
	RedeemedAt     int64
	ExpiresAt      int64
	RevokedAt      int64
	Label          string
	CreatedAt      int64
}

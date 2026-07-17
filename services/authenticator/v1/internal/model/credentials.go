package model

type UserCredential struct {
	UserID         int64
	HashedPassword string
	CreatedAt      int64
	UpdatedAt      int64
}

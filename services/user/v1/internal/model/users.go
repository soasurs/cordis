package model

type User struct {
	UserID         int64  `json:"user_id"`
	Email          string `json:"email"`
	HashedPassword string `json:"-"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	DeletedAt      int64  `json:"deleted_at"`
}

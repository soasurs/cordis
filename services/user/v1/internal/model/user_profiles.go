package model

type UserProfile struct {
	UserID        int64  `json:"user_id"`
	Username      string `json:"username"`
	Name          string `json:"name"`
	AvatarAssetID int64  `json:"avatar_asset_id"`
	CreatedAt     int64  `json:"created_at"`
	UpdatedAt     int64  `json:"updated_at"`
	DeletedAt     int64  `json:"deleted_at"`
}

package model

type Session struct {
	SessionID        int64  `json:"session_id"`
	UserID           int64  `json:"user_id"`
	RefreshTokenHash string `json:"-"`
	UserAgent        string `json:"user_agent"`
	IP               string `json:"ip"`
	CreatedAt        int64  `json:"created_at"`
	UpdatedAt        int64  `json:"updated_at"`
	ExpiresAt        int64  `json:"expires_at"`
	RevokedAt        int64  `json:"revoked_at"`
}

package model

type Guild struct {
	ID        int64
	OwnerID   int64
	Name      string
	IconURI   string
	Revision  int64
	CreatedAt int64
	UpdatedAt int64
	DeletedAt int64
}

type GuildMember struct {
	GuildID   int64
	UserID    int64
	Nickname  string
	Revision  int64
	JoinedAt  int64
	UpdatedAt int64
	DeletedAt int64
}

type Role struct {
	ID          int64
	GuildID     int64
	Name        string
	Permissions int64
	Position    int32
	IsDefault   bool
	Revision    int64
	CreatedAt   int64
	UpdatedAt   int64
	DeletedAt   int64
}

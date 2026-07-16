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
	Permissions uint64
	Position    int32
	IsDefault   bool
	Revision    int64
	CreatedAt   int64
	UpdatedAt   int64
	DeletedAt   int64
}

type Channel struct {
	ID        int64
	GuildID   int64
	Name      string
	Type      int32
	Position  int32
	Topic     string
	Revision  int64
	CreatedAt int64
	UpdatedAt int64
	DeletedAt int64
}

type ChannelPermissionOverwrite struct {
	ChannelID  int64
	GuildID    int64
	TargetType int32
	TargetID   int64
	Allow      uint64
	Deny       uint64
	Revision   int64
	CreatedAt  int64
	UpdatedAt  int64
}

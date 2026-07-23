package model

type Message struct {
	ID                  int64
	ChannelID           int64
	AuthorID            int64
	Content             string
	Type                int32
	Flags               int32
	ReferencedMessageID int64
	ReferencedChannelID int64
	Attachments         []Attachment
	EditedAt            int64
	CreatedAt           int64
	UpdatedAt           int64
	Revision            int64
	DeletedAt           int64
}

type Attachment struct {
	AssetID      int64
	Filename     string
	Size         int64
	ContentType  string
	Width        int32
	Height       int32
	URL          string
	URLExpiresAt int64
}

// DmChannel is a private 1:1 conversation; participants are stored in
// ascending user ID order.
type DmChannel struct {
	ID        int64
	UserLo    int64
	UserHi    int64
	CreatedAt int64
}

// Participates reports whether userID is one of the two members.
func (c *DmChannel) Participates(userID int64) bool {
	return userID == c.UserLo || userID == c.UserHi
}

// OtherParticipant returns the peer of userID.
func (c *DmChannel) OtherParticipant(userID int64) int64 {
	if userID == c.UserLo {
		return c.UserHi
	}
	return c.UserLo
}

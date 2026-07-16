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
	Key         string
	Filename    string
	Size        int64
	ContentType string
	Width       int32
	Height      int32
}

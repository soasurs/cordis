package model

type ChannelReadState struct {
	UserID            int64
	ChannelID         int64
	LastMessageID     int64
	LastReadMessageID int64
	UpdatedAt         int64
	MentionCount      int32
}

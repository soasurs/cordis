package model

type ChannelReadState struct {
	UserID            int64
	ChannelID         int64
	LastReadMessageID int64
	UpdatedAt         int64
	// Computed in GetReadStates, not persisted.
	MentionCount int32
	MessageCount int32
}

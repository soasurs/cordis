package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

const createMessageOperation = "message.create"

func normalizeMentionUserIDs(userIDs []int64) []int64 {
	values := make([]int64, 0, len(userIDs))
	values = append(values, userIDs...)
	slices.Sort(values)
	return values
}

type createMessageFingerprint struct {
	Version             int     `json:"version"`
	ChannelID           int64   `json:"channel_id"`
	Content             string  `json:"content"`
	Type                int64   `json:"type"`
	Flags               int64   `json:"flags"`
	ReferencedMessageID int64   `json:"referenced_message_id"`
	ReferencedChannelID int64   `json:"referenced_channel_id"`
	AttachmentAssetIDs  []int64 `json:"attachment_asset_ids"`
	MentionUserIDs      []int64 `json:"mention_user_ids"`
	MentionRoleIDs      []int64 `json:"mention_role_ids"`
	MentionEveryone     bool    `json:"mention_everyone"`
}

func createMessageRequestHash(
	channelID int64,
	content string,
	messageType messagev1.MessageType,
	flags int32,
	referencedMessageID, referencedChannelID int64,
	attachments []model.Attachment,
	mentions model.MessageMentions,
) ([]byte, error) {
	attachmentAssetIDs := make([]int64, 0, len(attachments))
	for _, attachment := range attachments {
		attachmentAssetIDs = append(attachmentAssetIDs, attachment.AssetID)
	}
	value := createMessageFingerprint{
		Version:             2,
		ChannelID:           channelID,
		Content:             content,
		Type:                int64(messageType),
		Flags:               int64(flags),
		ReferencedMessageID: referencedMessageID,
		ReferencedChannelID: referencedChannelID,
		AttachmentAssetIDs:  attachmentAssetIDs,
		MentionUserIDs:      normalizeMentionUserIDs(mentions.UserIDs),
		MentionRoleIDs:      normalizeMentionUserIDs(mentions.RoleIDs),
		MentionEveryone:     mentions.Everyone,
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal create message fingerprint: %w", err)
	}
	digest := sha256.Sum256(data)
	return digest[:], nil
}

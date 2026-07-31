package server

import (
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

func messageToProto(message *model.Message) *messagev1.Message {
	result := new(messagev1.Message)
	result.SetId(message.ID)
	result.SetChannelId(message.ChannelID)
	result.SetAuthorId(message.AuthorID)
	result.SetContent(message.Content)
	result.SetType(messagev1.MessageType(message.Type))
	result.SetFlags(message.Flags)
	result.SetReferencedMessageId(message.ReferencedMessageID)
	result.SetReferencedChannelId(message.ReferencedChannelID)
	result.SetAttachments(attachmentsToProto(message.Attachments))
	result.SetEditedAt(message.EditedAt)
	result.SetCreatedAt(message.CreatedAt)
	result.SetUpdatedAt(message.UpdatedAt)
	result.SetRevision(message.Revision)
	result.SetMentionUserIds(message.Mentions.UserIDs)
	result.SetMentionRoleIds(message.Mentions.RoleIDs)
	result.SetMentionEveryone(message.Mentions.Everyone)
	return result
}

func attachmentsToProto(attachments []model.Attachment) []*messagev1.Attachment {
	values := make([]*messagev1.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		result := new(messagev1.Attachment)
		result.SetAssetId(attachment.AssetID)
		result.SetFilename(attachment.Filename)
		result.SetSize(attachment.Size)
		result.SetContentType(attachment.ContentType)
		result.SetWidth(attachment.Width)
		result.SetHeight(attachment.Height)
		result.SetBlurhash(attachment.Blurhash)
		result.SetUrl(attachment.URL)
		result.SetUrlExpiresAt(attachment.URLExpiresAt)
		values = append(values, result)
	}
	return values
}

package server

import (
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

func toPBMessage(message *model.Message) *messagev1.Message {
	pbMessage := new(messagev1.Message)
	pbMessage.SetId(message.ID)
	pbMessage.SetChannelId(message.ChannelID)
	pbMessage.SetAuthorId(message.AuthorID)
	pbMessage.SetContent(message.Content)
	pbMessage.SetType(messagev1.MessageType(message.Type))
	pbMessage.SetFlags(message.Flags)
	pbMessage.SetReferencedMessageId(message.ReferencedMessageID)
	pbMessage.SetReferencedChannelId(message.ReferencedChannelID)
	pbMessage.SetAttachments(toPBAttachments(message.Attachments))
	pbMessage.SetEditedAt(message.EditedAt)
	pbMessage.SetCreatedAt(message.CreatedAt)
	pbMessage.SetUpdatedAt(message.UpdatedAt)
	return pbMessage
}

func toPBAttachments(attachments []model.Attachment) []*messagev1.Attachment {
	values := make([]*messagev1.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		pbAttachment := new(messagev1.Attachment)
		pbAttachment.SetKey(attachment.Key)
		pbAttachment.SetFilename(attachment.Filename)
		pbAttachment.SetSize(attachment.Size)
		pbAttachment.SetContentType(attachment.ContentType)
		pbAttachment.SetWidth(attachment.Width)
		pbAttachment.SetHeight(attachment.Height)
		values = append(values, pbAttachment)
	}
	return values
}

func toModelAttachments(attachments []*messagev1.Attachment) []model.Attachment {
	values := make([]model.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		values = append(values, model.Attachment{
			Key:         attachment.GetKey(),
			Filename:    attachment.GetFilename(),
			Size:        attachment.GetSize(),
			ContentType: attachment.GetContentType(),
			Width:       attachment.GetWidth(),
			Height:      attachment.GetHeight(),
		})
	}
	return values
}

func toPBReactionSummary(summary *model.ReactionSummary) *messagev1.ReactionSummary {
	pbSummary := new(messagev1.ReactionSummary)
	pbEmoji := new(messagev1.Emoji)
	pbEmoji.SetId(summary.Emoji.ID)
	pbEmoji.SetName(summary.Emoji.Name)
	pbEmoji.SetAnimated(summary.Emoji.Animated)
	pbEmoji.SetImageUrl(summary.Emoji.ImageURL)
	pbSummary.SetEmoji(pbEmoji)
	pbSummary.SetCount(summary.Count)
	pbSummary.SetMe(summary.Me)
	return pbSummary
}

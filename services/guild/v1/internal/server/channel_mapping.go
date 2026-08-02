package server

import (
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func guildChannelToProto(channel *model.Channel) *guildv1.GuildChannel {
	if channel == nil {
		return nil
	}
	value := new(guildv1.GuildChannel)
	value.SetId(channel.ID)
	value.SetGuildId(channel.GuildID)
	value.SetName(channel.Name)
	value.SetType(guildv1.GuildChannelType(channel.Type))
	value.SetPosition(channel.Position)
	value.SetTopic(channel.Topic)
	value.SetRevision(channel.Revision)
	value.SetCreatedAt(channel.CreatedAt)
	value.SetUpdatedAt(channel.UpdatedAt)
	value.SetParentId(channel.ParentID)
	return value
}

func guildChannelsToProto(channels []*model.Channel) []*guildv1.GuildChannel {
	values := make([]*guildv1.GuildChannel, 0, len(channels))
	for _, channel := range channels {
		values = append(values, guildChannelToProto(channel))
	}
	return values
}

func guildChannelOverwriteToProto(overwrite *model.ChannelPermissionOverwrite) *guildv1.GuildChannelPermissionOverwrite {
	if overwrite == nil {
		return nil
	}
	value := new(guildv1.GuildChannelPermissionOverwrite)
	value.SetChannelId(overwrite.ChannelID)
	value.SetGuildId(overwrite.GuildID)
	value.SetAppliesTo(guildv1.GuildPermissionOverwriteType(overwrite.AppliesTo))
	value.SetAppliesToId(overwrite.AppliesToID)
	value.SetAllow(overwrite.Allow)
	value.SetDeny(overwrite.Deny)
	value.SetRevision(overwrite.Revision)
	value.SetCreatedAt(overwrite.CreatedAt)
	value.SetUpdatedAt(overwrite.UpdatedAt)
	return value
}

func guildChannelOverwritesToProto(overwrites []*model.ChannelPermissionOverwrite) []*guildv1.GuildChannelPermissionOverwrite {
	values := make([]*guildv1.GuildChannelPermissionOverwrite, 0, len(overwrites))
	for _, overwrite := range overwrites {
		values = append(values, guildChannelOverwriteToProto(overwrite))
	}
	return values
}

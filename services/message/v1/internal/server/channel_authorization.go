package server

import (
	"context"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
)

const (
	permissionViewChannel    = uint64(guildv1.GuildPermission_GUILD_PERMISSION_VIEW_CHANNEL)
	permissionSendMessages   = uint64(guildv1.GuildPermission_GUILD_PERMISSION_SEND_MESSAGES)
	permissionManageMessages = uint64(guildv1.GuildPermission_GUILD_PERMISSION_MANAGE_MESSAGES)
)

func (s *messageServer) requireChannelPermission(ctx context.Context, channelID, userID int64, permission uint64) error {
	req := new(guildv1.AuthorizeGuildChannelRequest)
	req.SetChannelId(channelID)
	req.SetUserId(userID)
	req.SetPermission(permission)
	resp, err := s.svcCtx.GuildClient.AuthorizeGuildChannel(ctx, req)
	if err != nil {
		return err
	}
	if !resp.GetAllowed() {
		return permissionDenied()
	}
	if resp.GetChannelType() == guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY ||
		resp.GetChannelType() == guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_VOICE {
		return invalidRequest("messages are only supported in text channels")
	}
	return nil
}

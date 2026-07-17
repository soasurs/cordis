package server

import (
	"context"
	"database/sql"
	"errors"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
)

const (
	permissionViewChannel    = uint64(guildv1.GuildPermission_GUILD_PERMISSION_VIEW_CHANNEL)
	permissionSendMessages   = uint64(guildv1.GuildPermission_GUILD_PERMISSION_SEND_MESSAGES)
	permissionManageMessages = uint64(guildv1.GuildPermission_GUILD_PERMISSION_MANAGE_MESSAGES)
)

func (s *messageServer) requireChannelPermission(ctx context.Context, channelID, userID int64, permission uint64) error {
	// DM channels are owned locally; a hit settles authorization without a
	// Guild round trip. A miss falls through to the guild path.
	dmChannel, err := s.svcCtx.Store.GetDmChannel(ctx, channelID)
	if err == nil {
		return s.authorizeDmMessage(ctx, dmChannel, userID, permission)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

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

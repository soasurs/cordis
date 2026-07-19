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

type messageAudience struct {
	guildID int64
	userIDs []int64
}

func (s *messageServer) requireChannelPermission(
	ctx context.Context,
	channelID, userID int64,
	permission uint64,
) (messageAudience, error) {
	// DM channels are owned locally; a hit settles authorization without a
	// Guild round trip. A miss falls through to the guild path.
	dmChannel, err := s.svcCtx.Store.GetDmChannel(ctx, channelID)
	if err == nil {
		if err := s.authorizeDmMessage(ctx, dmChannel, userID, permission); err != nil {
			return messageAudience{}, err
		}
		return messageAudience{userIDs: []int64{dmChannel.UserLo, dmChannel.UserHi}}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return messageAudience{}, err
	}

	req := new(guildv1.AuthorizeGuildChannelRequest)
	req.SetChannelId(channelID)
	req.SetUserId(userID)
	req.SetPermission(permission)
	resp, err := s.svcCtx.GuildClient.AuthorizeGuildChannel(ctx, req)
	if err != nil {
		return messageAudience{}, err
	}
	if !resp.GetAllowed() {
		return messageAudience{}, permissionDenied()
	}
	if resp.GetChannelType() == guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY ||
		resp.GetChannelType() == guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_VOICE {
		return messageAudience{}, invalidRequest("messages are only supported in text channels")
	}
	if resp.GetGuildId() <= 0 {
		return messageAudience{}, errors.New("guild channel authorization returned invalid guild id")
	}
	return messageAudience{guildID: resp.GetGuildId()}, nil
}

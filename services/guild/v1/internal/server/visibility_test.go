package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func TestGetUserReadyStateReturnsCompleteVisibleGuildState(t *testing.T) {
	fake := newFakeStore()
	for _, guildID := range []int64{20, 30} {
		guild := testGuild(guildID, 1001)
		guild.AccessRevision = guildID + 7
		fake.guilds[guildID] = guild
		fake.members[guildID] = testMembers(guildID, 1002)
		fake.roles[guildID] = map[int64]*model.Role{
			guildID: {
				ID: guildID, GuildID: guildID, IsDefault: true,
				Permissions: PermissionViewChannel, Revision: 1,
			},
		}
		fake.channels[guildID*10+1] = &model.Channel{ID: guildID*10 + 1, GuildID: guildID, Type: 1, Position: 1}
		fake.channels[guildID*10+2] = &model.Channel{ID: guildID*10 + 2, GuildID: guildID, Type: 1, Position: 2}
	}
	fake.roles[30][31] = &model.Role{
		ID: 31, GuildID: 30, Name: "member", Permissions: PermissionViewChannel,
		Position: 1, Revision: 1,
	}
	fake.memberRoles[30] = map[int64]map[int64]bool{1002: {31: true}}
	fake.overwrites[302] = map[string]*model.ChannelPermissionOverwrite{
		"member": {
			ChannelID: 302, GuildID: 30, TargetType: int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER),
			TargetID: 1002, Deny: PermissionViewChannel,
		},
	}
	fake.overwrites[301] = map[string]*model.ChannelPermissionOverwrite{
		"everyone": {
			ChannelID: 301, GuildID: 30, TargetType: int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE),
			TargetID: 30, Allow: PermissionViewChannel,
		},
	}

	server := newTestGuildServer(t, fake, nil)
	req := new(guildv1.GetUserReadyStateRequest)
	req.SetUserId(1002)
	resp, err := server.GetUserReadyState(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetGuilds(), 2)
	require.Equal(t, int64(30), resp.GetGuilds()[0].GetGuild().GetId())
	require.Equal(t, int64(37), resp.GetGuilds()[0].GetAccessRevision())
	require.Len(t, resp.GetGuilds()[0].GetRoles(), 2)
	require.Equal(t, []int64{31}, resp.GetGuilds()[0].GetMemberRoleIds())
	require.Equal(t, []int64{301}, channelIDs(resp.GetGuilds()[0].GetChannels()))
	require.Len(t, resp.GetGuilds()[0].GetPermissionOverwrites(), 1)
	require.Equal(t, int64(301), resp.GetGuilds()[0].GetPermissionOverwrites()[0].GetChannelId())
	require.Equal(t, int64(20), resp.GetGuilds()[1].GetGuild().GetId())
	require.Equal(t, []int64{201, 202}, channelIDs(resp.GetGuilds()[1].GetChannels()))
}

func TestGetUserReadyStateValidatesUser(t *testing.T) {
	server := newTestGuildServer(t, newFakeStore(), nil)
	_, err := server.GetUserReadyState(t.Context(), new(guildv1.GetUserReadyStateRequest))
	require.Error(t, err)
}

func channelIDs(channels []*guildv1.GuildChannel) []int64 {
	ids := make([]int64, 0, len(channels))
	for _, channel := range channels {
		ids = append(ids, channel.GetId())
	}
	return ids
}

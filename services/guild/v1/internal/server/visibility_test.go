package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func TestListUserGuildChannelVisibilitiesFiltersAndPaginates(t *testing.T) {
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
		fake.channels[guildID*10+1] = &model.Channel{ID: guildID*10 + 1, GuildID: guildID, Type: 1}
		fake.channels[guildID*10+2] = &model.Channel{ID: guildID*10 + 2, GuildID: guildID, Type: 1}
	}
	fake.overwrites[302] = map[string]*model.ChannelPermissionOverwrite{
		"member": {
			ChannelID: 302, GuildID: 30, TargetType: int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER),
			TargetID: 1002, Deny: PermissionViewChannel,
		},
	}

	server := newTestGuildServer(t, fake, nil)
	req := new(guildv1.ListUserGuildChannelVisibilitiesRequest)
	req.SetUserId(1002)
	req.SetLimit(1)
	resp, err := server.ListUserGuildChannelVisibilities(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetVisibilities(), 1)
	require.Equal(t, int64(30), resp.GetVisibilities()[0].GetGuildId())
	require.Equal(t, int64(37), resp.GetVisibilities()[0].GetAccessRevision())
	require.Equal(t, []int64{301}, resp.GetVisibilities()[0].GetVisibleChannelIds())
	require.Equal(t, int64(30), resp.GetBeforeGuildId())

	req.SetBeforeGuildId(resp.GetBeforeGuildId())
	resp, err = server.ListUserGuildChannelVisibilities(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetVisibilities(), 1)
	require.Equal(t, int64(20), resp.GetVisibilities()[0].GetGuildId())
	require.Equal(t, []int64{201, 202}, resp.GetVisibilities()[0].GetVisibleChannelIds())
}

func TestListUserGuildChannelVisibilitiesValidatesRequest(t *testing.T) {
	server := newTestGuildServer(t, newFakeStore(), nil)
	req := new(guildv1.ListUserGuildChannelVisibilitiesRequest)
	_, err := server.ListUserGuildChannelVisibilities(t.Context(), req)
	require.Error(t, err)

	req.SetUserId(1001)
	req.SetBeforeGuildId(-1)
	_, err = server.ListUserGuildChannelVisibilities(t.Context(), req)
	require.Error(t, err)
}

//go:build integration

package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func TestListGuildMentionTargetsMatchesPerUserAuthorization(t *testing.T) {
	guildStore, service := newPostgresGuildService(t)

	const (
		guildID   = int64(20100)
		ownerID   = int64(20101)
		member2ID = int64(20102)
		member3ID = int64(20103)
		member4ID = int64(20104)
		roleAID   = int64(20120)
		roleBID   = int64(20121)
		channelID = int64(20150)
	)
	now := time.Now().UnixMilli()
	_, err := guildStore.CreateGuild(t.Context(), guildID, ownerID, "mentions", now)
	require.NoError(t, err)
	for _, userID := range []int64{ownerID, member2ID, member3ID, member4ID} {
		_, err = guildStore.CreateGuildMember(t.Context(), guildID, userID, now)
		require.NoError(t, err)
	}
	require.NoError(t, guildStore.CreateDefaultRole(t.Context(), guildID, now))
	_, err = guildStore.CreateGuildRole(t.Context(), roleAID, guildID, "a", 0, 2, now)
	require.NoError(t, err)
	_, err = guildStore.CreateGuildRole(t.Context(), roleBID, guildID, "b", 0, 2, now)
	require.NoError(t, err)
	require.NoError(t, guildStore.AddGuildMemberRole(t.Context(), guildID, member2ID, roleAID, now))
	require.NoError(t, guildStore.AddGuildMemberRole(t.Context(), guildID, member3ID, roleAID, now))
	require.NoError(t, guildStore.AddGuildMemberRole(t.Context(), guildID, member4ID, roleBID, now))
	_, err = guildStore.CreateGuildChannel(t.Context(), channelID, guildID, "general", int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT), 0, "", 0, now)
	require.NoError(t, err)

	allMembers := []int64{ownerID, member2ID, member3ID, member4ID}
	memberRoles := map[int64][]int64{
		ownerID:   {},
		member2ID: {roleAID},
		member3ID: {roleAID},
		member4ID: {roleBID},
	}
	assertEveryoneMatchesAuthorization := func(t *testing.T) {
		t.Helper()
		var want []int64
		for _, userID := range allMembers {
			req := new(guildv1.AuthorizeGuildChannelRequest)
			req.SetChannelId(channelID)
			req.SetUserId(userID)
			req.SetPermission(uint64(guildv1.GuildPermission_GUILD_PERMISSION_VIEW_CHANNEL))
			resp, err := service.AuthorizeGuildChannel(t.Context(), req)
			require.NoError(t, err)
			if resp.GetAllowed() {
				want = append(want, userID)
			}
		}

		var got []int64
		req := new(guildv1.ListGuildMentionTargetsRequest)
		req.SetGuildId(guildID)
		req.SetActorUserId(ownerID)
		req.SetChannelId(channelID)
		req.SetEveryone(true)
		for {
			resp, err := service.ListGuildMentionTargets(t.Context(), req)
			require.NoError(t, err)
			got = append(got, resp.GetUserIds()...)
			if !resp.HasNextCursor() {
				break
			}
			req.SetCursor(resp.GetNextCursor())
		}
		require.Equal(t, want, got)
	}
	assertRoleMatchesAuthorization := func(t *testing.T, roleIDs ...int64) {
		t.Helper()
		roleSet := make(map[int64]struct{}, len(roleIDs))
		for _, roleID := range roleIDs {
			roleSet[roleID] = struct{}{}
		}
		var want []int64
		for _, userID := range allMembers {
			hasRole := false
			for _, roleID := range memberRoles[userID] {
				if _, ok := roleSet[roleID]; ok {
					hasRole = true
					break
				}
			}
			if !hasRole {
				continue
			}
			req := new(guildv1.AuthorizeGuildChannelRequest)
			req.SetChannelId(channelID)
			req.SetUserId(userID)
			req.SetPermission(uint64(guildv1.GuildPermission_GUILD_PERMISSION_VIEW_CHANNEL))
			resp, err := service.AuthorizeGuildChannel(t.Context(), req)
			require.NoError(t, err)
			if resp.GetAllowed() {
				want = append(want, userID)
			}
		}

		var got []int64
		req := new(guildv1.ListGuildMentionTargetsRequest)
		req.SetGuildId(guildID)
		req.SetActorUserId(ownerID)
		req.SetChannelId(channelID)
		req.SetEveryone(false)
		req.SetRoleIds(roleIDs)
		req.SetLimit(2)
		for {
			resp, err := service.ListGuildMentionTargets(t.Context(), req)
			require.NoError(t, err)
			got = append(got, resp.GetUserIds()...)
			if !resp.HasNextCursor() {
				break
			}
			req.SetCursor(resp.GetNextCursor())
		}
		require.Equal(t, want, got)
	}

	// Without overwrites every active member can see the channel.
	assertEveryoneMatchesAuthorization(t)
	assertRoleMatchesAuthorization(t, roleAID)
	assertRoleMatchesAuthorization(t, roleBID)
	assertRoleMatchesAuthorization(t, roleAID, roleBID)

	// A role-level deny hides only role members.
	_, err = guildStore.UpsertGuildChannelPermissionOverwrite(t.Context(), &model.ChannelPermissionOverwrite{
		ChannelID:   channelID,
		GuildID:     guildID,
		AppliesTo:   int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE),
		AppliesToID: roleAID,
		Deny:        uint64(guildv1.GuildPermission_GUILD_PERMISSION_VIEW_CHANNEL),
		Revision:    1,
		CreatedAt:   now,
	})
	require.NoError(t, err)
	assertEveryoneMatchesAuthorization(t)
	assertRoleMatchesAuthorization(t, roleAID)
	assertRoleMatchesAuthorization(t, roleBID)
	assertRoleMatchesAuthorization(t, roleAID, roleBID)
}

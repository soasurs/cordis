package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func mentionSearchTestStore() *fakeStore {
	fakeStore := roleTestStore()
	fakeStore.channels[200] = &model.Channel{ID: 200, GuildID: 10, Name: "general", Type: 1, Revision: 1, CreatedAt: 1}
	fakeStore.profiles[10] = make(map[int64]*model.GuildMemberProfile)
	for _, userID := range []int64{1001, 1002, 1003, 1004} {
		fakeStore.profiles[10][userID] = &model.GuildMemberProfile{
			GuildID: 10, UserID: userID,
			Username: "user_" + guildIDString(userID), Name: "Member " + guildIDString(userID),
			UsernameSearch: "user_" + guildIDString(userID), NameSearch: "member " + guildIDString(userID),
		}
	}
	return fakeStore
}

func TestSearchGuildMentionUsersFiltersChannelVisibilityAndFillsLimit(t *testing.T) {
	fakeStore := mentionSearchTestStore()
	fakeStore.roles[10][21] = testRole(21, 10, "hidden", 0, 2)
	for _, userID := range []int64{1002, 1003} {
		require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, userID, 21, 1))
	}
	_, err := fakeStore.UpsertGuildChannelPermissionOverwrite(t.Context(), &model.ChannelPermissionOverwrite{
		ChannelID: 200, GuildID: 10,
		AppliesTo: int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE), AppliesToID: 21,
		Deny: PermissionViewChannel, Revision: 1, CreatedAt: 1,
	})
	require.NoError(t, err)
	server := newTestGuildServer(t, fakeStore, nil)
	req := new(guildv1.SearchGuildMentionUsersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetChannelId(200)
	req.SetQuery("user_")
	req.SetLimit(2)

	resp, err := server.SearchGuildMentionUsers(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1001, 1004}, []int64{resp.GetUsers()[0].GetUserId(), resp.GetUsers()[1].GetUserId()})
}

func TestSearchGuildMentionUsersContinuesAfterInvisibleCandidateWindow(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.channels[200] = &model.Channel{ID: 200, GuildID: 10, Name: "general", Type: 1}
	fakeStore.roles[10][21] = testRole(21, 10, "hidden", 0, 2)
	fakeStore.profiles[10] = make(map[int64]*model.GuildMemberProfile)
	fakeStore.members[10] = make(map[int64]*model.GuildMember)
	for userID := int64(1001); userID <= 1120; userID++ {
		fakeStore.members[10][userID] = &model.GuildMember{GuildID: 10, UserID: userID, Revision: 1, JoinedAt: 1}
		username := "user_" + guildIDString(userID)
		fakeStore.profiles[10][userID] = &model.GuildMemberProfile{
			GuildID: 10, UserID: userID, Username: username, UsernameSearch: username,
		}
		if userID >= 1002 && userID <= 1100 {
			require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, userID, 21, 1))
		}
	}
	_, err := fakeStore.UpsertGuildChannelPermissionOverwrite(t.Context(), &model.ChannelPermissionOverwrite{
		ChannelID: 200, GuildID: 10,
		AppliesTo: int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE), AppliesToID: 21,
		Deny: PermissionViewChannel, Revision: 1, CreatedAt: 1,
	})
	require.NoError(t, err)
	server := newTestGuildServer(t, fakeStore, nil)
	req := new(guildv1.SearchGuildMentionUsersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetChannelId(200)
	req.SetQuery("user_")
	req.SetLimit(2)

	resp, err := server.SearchGuildMentionUsers(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1001, 1101}, []int64{resp.GetUsers()[0].GetUserId(), resp.GetUsers()[1].GetUserId()})
}

func TestSearchGuildMentionUsersMatchesProfileName(t *testing.T) {
	fakeStore := mentionSearchTestStore()
	fakeStore.profiles[10][1003].Username = "other"
	fakeStore.profiles[10][1003].UsernameSearch = "other"
	fakeStore.profiles[10][1003].Name = "Alice Example"
	fakeStore.profiles[10][1003].NameSearch = "alice example"
	server := newTestGuildServer(t, fakeStore, nil)
	req := new(guildv1.SearchGuildMentionUsersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetChannelId(200)
	req.SetQuery("ALICE")

	resp, err := server.SearchGuildMentionUsers(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetUsers(), 1)
	require.Equal(t, int64(1003), resp.GetUsers()[0].GetUserId())
}

func TestSearchGuildMentionUsersMatchesGuildNickname(t *testing.T) {
	fakeStore := mentionSearchTestStore()
	fakeStore.profiles[10][1003].Username = "other"
	fakeStore.profiles[10][1003].UsernameSearch = "other"
	fakeStore.profiles[10][1003].Name = "Member 1003"
	fakeStore.profiles[10][1003].NameSearch = "member 1003"
	fakeStore.profiles[10][1003].Nickname = "Alice Example"
	fakeStore.profiles[10][1003].NicknameSearch = "alice example"
	server := newTestGuildServer(t, fakeStore, nil)
	req := new(guildv1.SearchGuildMentionUsersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetChannelId(200)
	req.SetQuery("ALICE")

	resp, err := server.SearchGuildMentionUsers(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetUsers(), 1)
	require.Equal(t, int64(1003), resp.GetUsers()[0].GetUserId())
	require.Equal(t, "Alice Example", resp.GetUsers()[0].GetNickname())
}

func TestSearchGuildMentionRolesDoesNotFilterByTargetVisibility(t *testing.T) {
	fakeStore := mentionSearchTestStore()
	fakeStore.roles[10][21] = testRole(21, 10, "hidden", 0, 2)
	server := newTestGuildServer(t, fakeStore, nil)
	req := new(guildv1.SearchGuildMentionRolesRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetQuery("hid")

	resp, err := server.SearchGuildMentionRoles(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetRoles(), 1)
	require.Equal(t, int64(21), resp.GetRoles()[0].GetId())
}

func TestFilterGuildChannelVisibleUsers(t *testing.T) {
	fakeStore := mentionSearchTestStore()
	fakeStore.roles[10][21] = testRole(21, 10, "hidden", 0, 2)
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 21, 1))
	_, err := fakeStore.UpsertGuildChannelPermissionOverwrite(t.Context(), &model.ChannelPermissionOverwrite{
		ChannelID: 200, GuildID: 10,
		AppliesTo: int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE), AppliesToID: 21,
		Deny: PermissionViewChannel, Revision: 1, CreatedAt: 1,
	})
	require.NoError(t, err)
	server := newTestGuildServer(t, fakeStore, nil)
	req := new(guildv1.FilterGuildChannelVisibleUsersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetChannelId(200)
	req.SetUserIds([]int64{1002, 1001, 9999, 1001})

	resp, err := server.FilterGuildChannelVisibleUsers(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1001}, resp.GetUserIds())
}

func TestSearchGuildMentionUsersRequiresQuery(t *testing.T) {
	server := newTestGuildServer(t, mentionSearchTestStore(), nil)
	req := new(guildv1.SearchGuildMentionUsersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetChannelId(200)
	_, err := server.SearchGuildMentionUsers(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

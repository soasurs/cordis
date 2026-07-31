package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func mentionTargetsTestStore() *fakeStore {
	fakeStore := roleTestStore()
	fakeStore.channels[200] = &model.Channel{
		ID: 200, GuildID: 10, Name: "general", Type: 1, Revision: 1, CreatedAt: 1,
	}
	return fakeStore
}

func mentionTargetsRequest() *guildv1.ListGuildMentionTargetsRequest {
	req := new(guildv1.ListGuildMentionTargetsRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetChannelId(200)
	req.SetEveryone(true)
	return req
}

func TestListGuildMentionTargetsValidation(t *testing.T) {
	fakeStore := mentionTargetsTestStore()
	server := newTestGuildServer(t, fakeStore, new(fakePublisher))

	tests := []struct {
		name    string
		mutate  func(*guildv1.ListGuildMentionTargetsRequest)
		wantErr codes.Code
	}{
		{name: "guild id required", mutate: func(req *guildv1.ListGuildMentionTargetsRequest) { req.SetGuildId(0) }, wantErr: codes.InvalidArgument},
		{name: "actor required", mutate: func(req *guildv1.ListGuildMentionTargetsRequest) { req.SetActorUserId(0) }, wantErr: codes.InvalidArgument},
		{name: "channel required", mutate: func(req *guildv1.ListGuildMentionTargetsRequest) { req.SetChannelId(0) }, wantErr: codes.InvalidArgument},
		{name: "role id positive", mutate: func(req *guildv1.ListGuildMentionTargetsRequest) {
			req.SetEveryone(false)
			req.SetRoleIds([]int64{0})
		}, wantErr: codes.InvalidArgument},
		{name: "role ids unique", mutate: func(req *guildv1.ListGuildMentionTargetsRequest) {
			req.SetEveryone(false)
			req.SetRoleIds([]int64{21, 21})
		}, wantErr: codes.InvalidArgument},
		{name: "mention source required", mutate: func(req *guildv1.ListGuildMentionTargetsRequest) { req.SetEveryone(false) }, wantErr: codes.InvalidArgument},
		{name: "limit out of range", mutate: func(req *guildv1.ListGuildMentionTargetsRequest) { req.SetLimit(1001) }, wantErr: codes.InvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := mentionTargetsRequest()
			tt.mutate(req)
			_, err := server.ListGuildMentionTargets(t.Context(), req)
			require.Equal(t, tt.wantErr, status.Code(err))
		})
	}

	t.Run("actor must be member", func(t *testing.T) {
		req := mentionTargetsRequest()
		req.SetActorUserId(9999)
		_, err := server.ListGuildMentionTargets(t.Context(), req)
		require.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("channel must belong to guild", func(t *testing.T) {
		fakeStore.channels[201] = &model.Channel{ID: 201, GuildID: 11, Name: "foreign", Type: 1}
		req := mentionTargetsRequest()
		req.SetChannelId(201)
		_, err := server.ListGuildMentionTargets(t.Context(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("role must belong to guild", func(t *testing.T) {
		req := mentionTargetsRequest()
		req.SetEveryone(false)
		req.SetRoleIds([]int64{999})
		_, err := server.ListGuildMentionTargets(t.Context(), req)
		require.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestListGuildMentionTargetsEveryoneVisibility(t *testing.T) {
	fakeStore := mentionTargetsTestStore()
	server := newTestGuildServer(t, fakeStore, new(fakePublisher))

	req := mentionTargetsRequest()
	resp, err := server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1001, 1002, 1003, 1004}, resp.GetUserIds())
	require.False(t, resp.HasNextCursor())

	// A role-level deny removes only its assigned members from the audience.
	fakeStore.roles[10][21] = testRole(21, 10, "hidden", 0, 2)
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 21, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1003, 21, 1))
	_, err = fakeStore.UpsertGuildChannelPermissionOverwrite(t.Context(), &model.ChannelPermissionOverwrite{
		ChannelID: 200, GuildID: 10,
		AppliesTo:   int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE),
		AppliesToID: 21,
		Deny:        PermissionViewChannel,
		Revision:    1, CreatedAt: 1,
	})
	require.NoError(t, err)

	resp, err = server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1001, 1004}, resp.GetUserIds())
}

func TestListGuildMentionTargetsRoles(t *testing.T) {
	fakeStore := mentionTargetsTestStore()
	server := newTestGuildServer(t, fakeStore, new(fakePublisher))
	fakeStore.roles[10][21] = testRole(21, 10, "a", 0, 2)
	fakeStore.roles[10][22] = testRole(22, 10, "b", 0, 2)
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 21, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1004, 21, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1003, 22, 1))

	req := mentionTargetsRequest()
	req.SetEveryone(false)
	req.SetRoleIds([]int64{21})
	resp, err := server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1002, 1004}, resp.GetUserIds())

	req.SetRoleIds([]int64{22, 21})
	resp, err = server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1002, 1003, 1004}, resp.GetUserIds())
}

func TestListGuildMentionTargetsPagination(t *testing.T) {
	fakeStore := mentionTargetsTestStore()
	for _, userID := range []int64{1005, 1006, 1007, 1008, 1009, 1010} {
		fakeStore.members[10][userID] = testMembers(10, userID)[userID]
	}
	server := newTestGuildServer(t, fakeStore, new(fakePublisher))

	req := mentionTargetsRequest()
	req.SetLimit(3)
	resp, err := server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1001, 1002, 1003}, resp.GetUserIds())
	require.True(t, resp.HasNextCursor())

	req.SetCursor(resp.GetNextCursor())
	resp, err = server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1004, 1005, 1006}, resp.GetUserIds())
	require.True(t, resp.HasNextCursor())

	req.SetCursor(resp.GetNextCursor())
	resp, err = server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1007, 1008, 1009}, resp.GetUserIds())
	require.True(t, resp.HasNextCursor())

	req.SetCursor(resp.GetNextCursor())
	resp, err = server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1010}, resp.GetUserIds())
	require.False(t, resp.HasNextCursor())
}

func TestListGuildMentionTargetsPaginationSkipsInvisibleMembers(t *testing.T) {
	fakeStore := mentionTargetsTestStore()
	for _, userID := range []int64{1005, 1006, 1007, 1008, 1009, 1010} {
		fakeStore.members[10][userID] = testMembers(10, userID)[userID]
	}
	// Hide every non-owner member so visible results are sparse.
	fakeStore.roles[10][21] = testRole(21, 10, "hidden", 0, 2)
	for _, userID := range []int64{1002, 1003, 1004, 1005, 1006, 1007, 1008, 1009, 1010} {
		require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, userID, 21, 1))
	}
	_, err := fakeStore.UpsertGuildChannelPermissionOverwrite(t.Context(), &model.ChannelPermissionOverwrite{
		ChannelID: 200, GuildID: 10,
		AppliesTo:   int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE),
		AppliesToID: 21,
		Deny:        PermissionViewChannel,
		Revision:    1, CreatedAt: 1,
	})
	require.NoError(t, err)
	server := newTestGuildServer(t, fakeStore, new(fakePublisher))

	req := mentionTargetsRequest()
	req.SetLimit(3)
	resp, err := server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1001}, resp.GetUserIds())
	require.True(t, resp.HasNextCursor())

	// The next window is entirely invisible; the page is empty but the cursor
	// still advances past the scanned candidates.
	req.SetCursor(resp.GetNextCursor())
	resp, err = server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.Empty(t, resp.GetUserIds())
	require.True(t, resp.HasNextCursor())

	req.SetCursor(resp.GetNextCursor())
	resp, err = server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.Empty(t, resp.GetUserIds())
	require.True(t, resp.HasNextCursor())

	req.SetCursor(resp.GetNextCursor())
	resp, err = server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.Empty(t, resp.GetUserIds())
	require.False(t, resp.HasNextCursor())
}

func TestListGuildMentionTargetsCursorBindsRequestParameters(t *testing.T) {
	fakeStore := mentionTargetsTestStore()
	fakeStore.roles[10][21] = testRole(21, 10, "a", 0, 2)
	fakeStore.roles[10][22] = testRole(22, 10, "b", 0, 2)
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 21, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1004, 21, 1))
	server := newTestGuildServer(t, fakeStore, new(fakePublisher))

	req := mentionTargetsRequest()
	req.SetEveryone(false)
	req.SetRoleIds([]int64{21})
	req.SetLimit(1)
	resp, err := server.ListGuildMentionTargets(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.HasNextCursor())

	for _, tt := range []struct {
		name   string
		mutate func(*guildv1.ListGuildMentionTargetsRequest)
	}{
		{name: "different channel", mutate: func(req *guildv1.ListGuildMentionTargetsRequest) { req.SetChannelId(201) }},
		{name: "different roles", mutate: func(req *guildv1.ListGuildMentionTargetsRequest) { req.SetRoleIds([]int64{22}) }},
		{name: "different everyone flag", mutate: func(req *guildv1.ListGuildMentionTargetsRequest) { req.SetEveryone(true) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			req := mentionTargetsRequest()
			req.SetEveryone(false)
			req.SetRoleIds([]int64{21})
			req.SetLimit(1)
			req.SetCursor(resp.GetNextCursor())
			tt.mutate(req)
			_, err := server.ListGuildMentionTargets(t.Context(), req)
			require.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

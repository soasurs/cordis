package server

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func TestFilterUsersWithCommonGuild(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.guilds[20] = testGuild(20, 2001)
	fakeStore.members[10] = testMembers(10, 1001, 1002)
	fakeStore.members[20] = testMembers(20, 1001, 1003, 1004)
	fakeStore.members[20][1004].DeletedAt = 2
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.FilterUsersWithCommonGuildRequest)
	req.SetUserId(1001)
	req.SetTargetUserIds([]int64{1003, 1002, 1002, 1004, 1005})
	resp, err := server.FilterUsersWithCommonGuild(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1002, 1003}, resp.GetUserIds())
}

func TestFilterUsersWithCommonGuildValidatesBatch(t *testing.T) {
	server := newTestGuildServer(t, newFakeStore(), nil)

	req := new(guildv1.FilterUsersWithCommonGuildRequest)
	req.SetUserId(1001)
	req.SetTargetUserIds([]int64{0})
	_, err := server.FilterUsersWithCommonGuild(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetTargetUserIds(slices.Repeat([]int64{1002}, maxCommonGuildFilterBatch+1))
	_, err = server.FilterUsersWithCommonGuild(t.Context(), req)
	require.NoError(t, err)

	targets := make([]int64, maxCommonGuildFilterBatch+1)
	for i := range targets {
		targets[i] = int64(i + 1)
	}
	req.SetTargetUserIds(targets)
	_, err = server.FilterUsersWithCommonGuild(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestAddGuildMemberRequiresOwnerAndPublishesEvent(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001)
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.AddGuildMemberRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetUserId(1002)
	resp, err := server.AddGuildMember(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1002), resp.GetMember().GetUserId())
	require.Equal(t, int64(1), resp.GetMember().GetRevision())

	var envelope eventEnvelope[guildMemberPayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeGuildMemberJoined, envelope.Type)
	require.Equal(t, "10", envelope.Data.GuildID)
	require.Equal(t, "1002", envelope.Data.UserID)
	require.Equal(t, "1002", envelope.Data.Profile.UserID)
	require.Equal(t, "Bio 1002", envelope.Data.Profile.Bio)

	req.SetActorUserId(1002)
	req.SetUserId(1003)
	_, err = server.AddGuildMember(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestAddGuildMemberRejectsActiveDuplicateAndAllowsRejoin(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002)
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.AddGuildMemberRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetUserId(1002)
	_, err := server.AddGuildMember(t.Context(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))

	fakeStore.members[10][1002].DeletedAt = 2
	resp, err := server.AddGuildMember(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(2), resp.GetMember().GetRevision())
	require.Zero(t, fakeStore.members[10][1002].DeletedAt)
}

func TestUpdateGuildMemberUpdatesOnlyActor(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002)
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.UpdateGuildMemberRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1002)
	req.SetNickname(" Member ")
	resp, err := server.UpdateGuildMember(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "Member", resp.GetMember().GetNickname())
	require.Equal(t, int64(2), resp.GetMember().GetRevision())

	var envelope eventEnvelope[guildMemberPayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeGuildMemberUpdated, envelope.Type)
	require.Equal(t, "1002", envelope.Data.UserID)
	require.Equal(t, "1002", envelope.Data.Profile.UserID)
	require.Equal(t, "Bio 1002", envelope.Data.Profile.Bio)
}

func TestKickAndLeaveEnforceOwnerRules(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002, 1003)
	fakeStore.channels[20] = &model.Channel{ID: 20, GuildID: 10, Name: "general", Type: 1}
	fakeStore.overwrites[20] = map[string]*model.ChannelPermissionOverwrite{
		overwriteKey(int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER), 1002): {
			ChannelID: 20, GuildID: 10,
			AppliesTo:   int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER),
			AppliesToID: 1002, Deny: PermissionViewChannel,
		},
	}
	server := newTestGuildServer(t, fakeStore, new(fakePublisher))

	kick := new(guildv1.KickGuildMemberRequest)
	kick.SetGuildId(10)
	kick.SetActorUserId(1002)
	kick.SetUserId(1003)
	_, err := server.KickGuildMember(t.Context(), kick)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	kick.SetActorUserId(1001)
	kick.SetUserId(1001)
	_, err = server.KickGuildMember(t.Context(), kick)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	leave := new(guildv1.LeaveGuildRequest)
	leave.SetGuildId(10)
	leave.SetUserId(1001)
	_, err = server.LeaveGuild(t.Context(), leave)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	leave.SetUserId(1002)
	resp, err := server.LeaveGuild(t.Context(), leave)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.NotZero(t, fakeStore.members[10][1002].DeletedAt)
	require.Empty(t, fakeStore.overwrites[20])
}

func TestTransferGuildOwnershipRequiresActiveMember(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002)
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.TransferGuildOwnershipRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetNewOwnerId(1003)
	_, err := server.TransferGuildOwnership(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))

	req.SetNewOwnerId(1002)
	resp, err := server.TransferGuildOwnership(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1002), resp.GetGuild().GetOwnerId())
	require.Equal(t, int64(2), resp.GetGuild().GetRevision())

	var envelope eventEnvelope[guildPayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeGuildUpdated, envelope.Type)
	require.Equal(t, "1002", envelope.Data.OwnerID)
}

func TestListGuildMembersRequiresMembershipAndUsesCursor(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002, 1003)
	server := newTestGuildServer(t, fakeStore, nil)

	codec := testCursorCodec(t)
	token, err := codec.Encode(cursor.KindGuildMembers, guildTimeIDPayload{GuildID: 10, Time: 1, ID: 1003})
	require.NoError(t, err)
	req := new(guildv1.ListGuildMembersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetCursor(token)
	req.SetLimit(1)
	resp, err := server.ListGuildMembers(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetMembers(), 1)
	require.Equal(t, int64(1002), resp.GetMembers()[0].GetUserId())
	next, err := codec.Encode(cursor.KindGuildMembers, guildTimeIDPayload{GuildID: 10, Time: 1, ID: 1002})
	require.NoError(t, err)
	require.Equal(t, next, resp.GetNextCursor())
}

func TestListGuildMembersPagesWithServerCursors(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002, 1003, 1004)
	server := newTestGuildServer(t, fakeStore, nil)

	seen := make([]int64, 0, 4)
	req := new(guildv1.ListGuildMembersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetLimit(1)
	for {
		resp, err := server.ListGuildMembers(t.Context(), req)
		require.NoError(t, err)
		require.Len(t, resp.GetMembers(), 1)
		seen = append(seen, resp.GetMembers()[0].GetUserId())
		if !resp.HasNextCursor() {
			break
		}
		req.SetCursor(resp.GetNextCursor())
	}
	require.Equal(t, []int64{1004, 1003, 1002, 1001}, seen)
}

func TestListGuildMembersRejectsEmptyCursor(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001)
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.ListGuildMembersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetCursor("")
	_, err := server.ListGuildMembers(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestListGuildMembersRejectsBadCursors(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001)
	server := newTestGuildServer(t, fakeStore, nil)

	assertRejectsBadCursors(t, cursor.KindGuildMembers, guildTimeIDPayload{GuildID: 10, Time: 1, ID: 1001}, func(token string) error {
		req := new(guildv1.ListGuildMembersRequest)
		req.SetGuildId(10)
		req.SetActorUserId(1001)
		req.SetCursor(token)
		_, err := server.ListGuildMembers(t.Context(), req)
		return err
	})
}

func TestListGuildMembersRejectsCrossGuildCursor(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001)
	server := newTestGuildServer(t, fakeStore, nil)

	codec := testCursorCodec(t)
	token, err := codec.Encode(cursor.KindGuildMembers, guildTimeIDPayload{GuildID: 99, Time: 1, ID: 1001})
	require.NoError(t, err)
	req := new(guildv1.ListGuildMembersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetCursor(token)
	_, err = server.ListGuildMembers(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

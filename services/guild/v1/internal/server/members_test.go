package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

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
}

func TestKickAndLeaveEnforceOwnerRules(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002, 1003)
	fakeStore.channels[20] = &model.Channel{ID: 20, GuildID: 10, Name: "general", Type: 1}
	fakeStore.overwrites[20] = map[string]*model.ChannelPermissionOverwrite{
		overwriteKey(int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER), 1002): {
			ChannelID: 20, GuildID: 10,
			TargetType: int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER),
			TargetID:   1002, Deny: PermissionViewChannel,
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

	req := new(guildv1.ListGuildMembersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetBeforeUserId(1003)
	req.SetLimit(1)
	resp, err := server.ListGuildMembers(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetMembers(), 1)
	require.Equal(t, int64(1002), resp.GetMembers()[0].GetUserId())
	require.Equal(t, int64(1002), resp.GetBeforeUserId())
}

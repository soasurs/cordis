package server

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
)

func TestCreateGuildFingerprintsAreStableAndDistinct(t *testing.T) {
	first, err := createGuildRequestHash("Cordis")
	require.NoError(t, err)
	second, err := createGuildRequestHash("Cordis")
	require.NoError(t, err)
	require.True(t, bytes.Equal(first, second))

	different, err := createGuildRequestHash("Other")
	require.NoError(t, err)
	require.False(t, bytes.Equal(first, different))

	inviteHash, err := createGuildInviteRequestHash(10, 5, 60_000)
	require.NoError(t, err)
	inviteHashRetry, err := createGuildInviteRequestHash(10, 5, 60_000)
	require.NoError(t, err)
	require.True(t, bytes.Equal(inviteHash, inviteHashRetry))
	inviteHashLater, err := createGuildInviteRequestHash(10, 5, 120_000)
	require.NoError(t, err)
	require.False(t, bytes.Equal(inviteHash, inviteHashLater))
}

func TestValidateIdempotencyKey(t *testing.T) {
	require.NoError(t, validateIdempotencyKey(false, "anything", 255))
	require.NoError(t, validateIdempotencyKey(true, "intent-1", 255))
	require.Equal(t, codes.InvalidArgument, status.Code(validateIdempotencyKey(true, "", 255)))
	require.Equal(t, codes.InvalidArgument, status.Code(validateIdempotencyKey(true, " intent", 255)))
	require.Equal(t, codes.InvalidArgument, status.Code(validateIdempotencyKey(true, "intent ", 255)))
	require.Equal(t, codes.InvalidArgument, status.Code(validateIdempotencyKey(true, "long", 3)))
}

func TestCreateGuildIdempotentReplayReturnsSameGuild(t *testing.T) {
	fakeStore := newFakeStore()
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")
	req.SetIdempotencyKey("guild-intent-1")
	first, err := server.CreateGuild(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, fakeStore.guilds, 1)

	replay, err := server.CreateGuild(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, first.GetGuild().GetId(), replay.GetGuild().GetId())
	require.Equal(t, first.GetGuild().GetName(), replay.GetGuild().GetName())
	require.Len(t, fakeStore.guilds, 1)
	require.Len(t, fakeStore.members[first.GetGuild().GetId()], 1)
	require.True(t, fakeStore.defaultRoles[first.GetGuild().GetId()])
	channels, err := fakeStore.ListGuildChannels(t.Context(), first.GetGuild().GetId())
	require.NoError(t, err)
	require.Len(t, channels, 4)
	require.Len(t, publisher.records, 1, "replay must not republish the creation event")
}

func TestCreateGuildRejectsIdempotencyKeyReuseWithDifferentParameters(t *testing.T) {
	fakeStore := newFakeStore()
	server := newTestGuildServer(t, fakeStore, nil)

	first := new(guildv1.CreateGuildRequest)
	first.SetOwnerId(1001)
	first.SetName("Cordis")
	first.SetIdempotencyKey("guild-intent-1")
	_, err := server.CreateGuild(t.Context(), first)
	require.NoError(t, err)

	second := new(guildv1.CreateGuildRequest)
	second.SetOwnerId(1001)
	second.SetName("Other")
	second.SetIdempotencyKey("guild-intent-1")
	_, err = server.CreateGuild(t.Context(), second)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	info, ok := rpcerror.Parse(err)
	require.True(t, ok)
	require.Equal(t, rpcerror.GuildIdempotencyKeyReused, info.Reason)
	require.Len(t, fakeStore.guilds, 1)
}

func TestCreateGuildIdempotencyIsScopedPerActor(t *testing.T) {
	fakeStore := newFakeStore()
	server := newTestGuildServer(t, fakeStore, nil)

	first := new(guildv1.CreateGuildRequest)
	first.SetOwnerId(1001)
	first.SetName("Cordis")
	first.SetIdempotencyKey("shared-key")
	firstResp, err := server.CreateGuild(t.Context(), first)
	require.NoError(t, err)

	second := new(guildv1.CreateGuildRequest)
	second.SetOwnerId(1002)
	second.SetName("Cordis")
	second.SetIdempotencyKey("shared-key")
	resp, err := server.CreateGuild(t.Context(), second)
	require.NoError(t, err)
	require.Len(t, fakeStore.guilds, 2)
	require.NotEqual(t, firstResp.GetGuild().GetId(), resp.GetGuild().GetId())
}

func TestCreateGuildRoleIdempotentReplayReturnsSameRole(t *testing.T) {
	fakeStore := roleTestStore()
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.CreateGuildRoleRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetName("Moderator")
	req.SetPermissions(PermissionManageMessages)
	req.SetIdempotencyKey("role-intent-1")
	first, err := server.CreateGuildRole(t.Context(), req)
	require.NoError(t, err)

	replay, err := server.CreateGuildRole(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, first.GetRole().GetId(), replay.GetRole().GetId())
	require.Equal(t, first.GetRole().GetPosition(), replay.GetRole().GetPosition())
	require.Len(t, fakeStore.roles[10], 2, "replay must not create another role")
	require.Len(t, publisher.records, 1, "replay must not republish the role event")

	// A later role still computes its position from the persisted state only.
	second := new(guildv1.CreateGuildRoleRequest)
	second.SetGuildId(10)
	second.SetActorUserId(1001)
	second.SetName("Another")
	second.SetPermissions(PermissionManageMessages)
	second.SetIdempotencyKey("role-intent-2")
	resp, err := server.CreateGuildRole(t.Context(), second)
	require.NoError(t, err)
	require.Equal(t, int32(2), resp.GetRole().GetPosition())
}

func TestCreateGuildRoleRejectsIdempotencyKeyReuseWithDifferentParameters(t *testing.T) {
	fakeStore := roleTestStore()
	server := newTestGuildServer(t, fakeStore, nil)

	first := new(guildv1.CreateGuildRoleRequest)
	first.SetGuildId(10)
	first.SetActorUserId(1001)
	first.SetName("Moderator")
	first.SetPermissions(PermissionManageMessages)
	first.SetIdempotencyKey("role-intent-1")
	_, err := server.CreateGuildRole(t.Context(), first)
	require.NoError(t, err)

	second := new(guildv1.CreateGuildRoleRequest)
	second.SetGuildId(10)
	second.SetActorUserId(1001)
	second.SetName("Admin")
	second.SetPermissions(PermissionManageMessages)
	second.SetIdempotencyKey("role-intent-1")
	_, err = server.CreateGuildRole(t.Context(), second)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Len(t, fakeStore.roles[10], 2, "reused key must not create another role")
}

func TestCreateGuildChannelIdempotentReplayReturnsSameChannel(t *testing.T) {
	fakeStore := roleTestStore()
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.CreateGuildChannelRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetExpectedChannelLayoutRevision(1)
	req.SetName("general")
	req.SetTopic("chat")
	req.SetIdempotencyKey("channel-intent-1")
	first, err := server.CreateGuildChannel(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, publisher.records, 2)

	replay, err := server.CreateGuildChannel(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, first.GetChannel().GetId(), replay.GetChannel().GetId())
	require.Equal(t, first.GetChannelLayoutRevision(), replay.GetChannelLayoutRevision())
	require.Equal(t, first.GetChannel().GetTopic(), replay.GetChannel().GetTopic())
	require.Len(t, fakeStore.channels, 1, "replay must not create another channel")
	require.Len(t, publisher.records, 2, "replay must not republish channel or overwrite events")
}

func TestCreateGuildChannelRejectsIdempotencyKeyReuseWithDifferentParameters(t *testing.T) {
	fakeStore := roleTestStore()
	server := newTestGuildServer(t, fakeStore, nil)

	first := new(guildv1.CreateGuildChannelRequest)
	first.SetGuildId(10)
	first.SetActorUserId(1001)
	first.SetExpectedChannelLayoutRevision(1)
	first.SetName("general")
	first.SetIdempotencyKey("channel-intent-1")
	_, err := server.CreateGuildChannel(t.Context(), first)
	require.NoError(t, err)

	second := new(guildv1.CreateGuildChannelRequest)
	second.SetGuildId(10)
	second.SetActorUserId(1001)
	second.SetExpectedChannelLayoutRevision(2)
	second.SetName("other")
	second.SetIdempotencyKey("channel-intent-1")
	_, err = server.CreateGuildChannel(t.Context(), second)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Len(t, fakeStore.channels, 1)
}

func TestCreateGuildInviteIdempotentReplayReturnsSameInvite(t *testing.T) {
	fakeStore := roleTestStore()
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.CreateGuildInviteRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetMaxUses(5)
	req.SetExpiresInMs(60_000)
	req.SetIdempotencyKey("invite-intent-1")
	first, err := server.CreateGuildInvite(t.Context(), req)
	require.NoError(t, err)

	replay, err := server.CreateGuildInvite(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, first.GetInvite().GetId(), replay.GetInvite().GetId())
	require.Equal(t, first.GetInvite().GetCode(), replay.GetInvite().GetCode())
	require.Equal(t, first.GetInvite().GetExpiresAt(), replay.GetInvite().GetExpiresAt())
	require.Len(t, fakeStore.invites, 1, "replay must not create another invite")
}

func TestCreateGuildRoleReplayRechecksAuthorization(t *testing.T) {
	fakeStore := roleTestStore()
	_, err := fakeStore.CreateGuildRole(context.Background(), 200, 10, "Mod", PermissionManageRoles, 5, 1)
	require.NoError(t, err)
	require.NoError(t, fakeStore.AddGuildMemberRole(context.Background(), 10, 1002, 200, 1))
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.CreateGuildRoleRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1002)
	req.SetName("Delegated")
	req.SetPermissions(0)
	req.SetIdempotencyKey("role-intent-auth")
	first, err := server.CreateGuildRole(t.Context(), req)
	require.NoError(t, err)

	require.NoError(t, fakeStore.RemoveGuildMemberRole(context.Background(), 10, 1002, 200))
	_, err = server.CreateGuildRole(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err), "replay must pass through authorization")
	require.NotEqual(t, first.GetRole().GetId(), int64(0))
}

func TestCreateGuildInviteRejectsIdempotencyKeyReuseWithDifferentParameters(t *testing.T) {
	fakeStore := roleTestStore()
	server := newTestGuildServer(t, fakeStore, nil)

	first := new(guildv1.CreateGuildInviteRequest)
	first.SetGuildId(10)
	first.SetActorUserId(1001)
	first.SetMaxUses(5)
	first.SetExpiresInMs(60_000)
	first.SetIdempotencyKey("invite-intent-1")
	_, err := server.CreateGuildInvite(t.Context(), first)
	require.NoError(t, err)

	second := new(guildv1.CreateGuildInviteRequest)
	second.SetGuildId(10)
	second.SetActorUserId(1001)
	second.SetMaxUses(10)
	second.SetExpiresInMs(60_000)
	second.SetIdempotencyKey("invite-intent-1")
	_, err = server.CreateGuildInvite(t.Context(), second)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Len(t, fakeStore.invites, 1)
}

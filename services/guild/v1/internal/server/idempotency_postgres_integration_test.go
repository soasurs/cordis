//go:build integration

package server

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func createGuildWithPostgres(t *testing.T, service guildv1.GuildServiceServer) int64 {
	t.Helper()
	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")
	resp, err := service.CreateGuild(t.Context(), req)
	require.NoError(t, err)
	return resp.GetGuild().GetId()
}

func TestCreateGuildIdempotentReplayWithPostgres(t *testing.T) {
	guildStore, service := newPostgresGuildService(t)
	ctx := t.Context()

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")
	req.SetIdempotencyKey("guild-intent-1")
	first, err := service.CreateGuild(ctx, req)
	require.NoError(t, err)
	guildID := first.GetGuild().GetId()

	replay, err := service.CreateGuild(ctx, req)
	require.NoError(t, err)
	require.Equal(t, guildID, replay.GetGuild().GetId())

	members, err := guildStore.ListGuildMembers(ctx, store.ListGuildMembersParams{GuildID: guildID, Limit: 100})
	require.NoError(t, err)
	require.Len(t, members, 1, "replay must not create another owner member")
	roles, err := guildStore.ListGuildRoles(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, roles, 1, "replay must not create another default role")
	channels, err := guildStore.ListGuildChannels(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, channels, 4, "replay must not recreate default channels")
	overwrites, err := guildStore.ListGuildChannelPermissionOverwritesByGuild(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, overwrites, 4, "replay must not recreate default overwrites")

	guilds, err := guildStore.ListUserGuilds(ctx, store.ListUserGuildsParams{UserID: 1001, Limit: 100})
	require.NoError(t, err)
	require.Len(t, guilds, 1)
}

func TestCreateGuildRejectsIdempotencyKeyReuseWithPostgres(t *testing.T) {
	guildStore, service := newPostgresGuildService(t)
	ctx := t.Context()

	first := new(guildv1.CreateGuildRequest)
	first.SetOwnerId(1001)
	first.SetName("Cordis")
	first.SetIdempotencyKey("guild-intent-1")
	_, err := service.CreateGuild(ctx, first)
	require.NoError(t, err)

	second := new(guildv1.CreateGuildRequest)
	second.SetOwnerId(1001)
	second.SetName("Other")
	second.SetIdempotencyKey("guild-intent-1")
	_, err = service.CreateGuild(ctx, second)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	info, ok := rpcerror.Parse(err)
	require.True(t, ok)
	require.Equal(t, rpcerror.GuildIdempotencyKeyReused, info.Reason)

	guilds, err := guildStore.ListUserGuilds(ctx, store.ListUserGuildsParams{UserID: 1001, Limit: 100})
	require.NoError(t, err)
	require.Len(t, guilds, 1, "reused key must not create another guild")
}

func TestCreateGuildConcurrentIdempotentRequestsWithPostgres(t *testing.T) {
	guildStore, service := newPostgresGuildService(t)
	ctx := t.Context()

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")
	req.SetIdempotencyKey("guild-intent-concurrent")

	var wg sync.WaitGroup
	guildIDs := make([]int64, 2)
	errs := make([]error, 2)
	start := make(chan struct{})
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			resp, err := service.CreateGuild(ctx, req)
			errs[i] = err
			if resp != nil {
				guildIDs[i] = resp.GetGuild().GetId()
			}
		}(i)
	}
	close(start)
	wg.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	require.NotZero(t, guildIDs[0])
	require.Equal(t, guildIDs[0], guildIDs[1], "concurrent same-key requests must return the same guild")

	guilds, err := guildStore.ListUserGuilds(ctx, store.ListUserGuildsParams{UserID: 1001, Limit: 100})
	require.NoError(t, err)
	require.Len(t, guilds, 1, "concurrent same-key requests must create one guild")
}

func TestCreateGuildChannelIdempotentReplayDoesNotShiftAgainWithPostgres(t *testing.T) {
	guildStore, service := newPostgresGuildService(t)
	ctx := t.Context()
	guildID := createGuildWithPostgres(t, service)

	first := new(guildv1.CreateGuildChannelRequest)
	first.SetGuildId(guildID)
	first.SetActorUserId(1001)
	first.SetExpectedChannelLayoutRevision(1)
	first.SetName("uncat")
	first.SetIdempotencyKey("channel-intent-1")
	created, err := service.CreateGuildChannel(ctx, first)
	require.NoError(t, err)

	channelsAfterFirst, err := guildStore.ListGuildChannels(ctx, guildID)
	require.NoError(t, err)
	positions := make(map[int64]int32, len(channelsAfterFirst))
	for _, channel := range channelsAfterFirst {
		positions[channel.ID] = channel.Position
	}

	replay, err := service.CreateGuildChannel(ctx, first)
	require.NoError(t, err)
	require.Equal(t, created.GetChannel().GetId(), replay.GetChannel().GetId())

	channelsAfterReplay, err := guildStore.ListGuildChannels(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, channelsAfterReplay, len(channelsAfterFirst))
	for _, channel := range channelsAfterReplay {
		require.Equal(t, positions[channel.ID], channel.Position, "replay must not shift channels again")
	}
}

func TestCreateGuildInviteIdempotentReplayReturnsSameInviteWithPostgres(t *testing.T) {
	guildStore, service := newPostgresGuildService(t)
	ctx := t.Context()
	guildID := createGuildWithPostgres(t, service)

	req := new(guildv1.CreateGuildInviteRequest)
	req.SetGuildId(guildID)
	req.SetActorUserId(1001)
	req.SetMaxUses(5)
	req.SetExpiresInMs(60_000)
	req.SetIdempotencyKey("invite-intent-1")
	first, err := service.CreateGuildInvite(ctx, req)
	require.NoError(t, err)

	replay, err := service.CreateGuildInvite(ctx, req)
	require.NoError(t, err)
	require.Equal(t, first.GetInvite().GetId(), replay.GetInvite().GetId())
	require.Equal(t, first.GetInvite().GetCode(), replay.GetInvite().GetCode())
	require.Equal(t, first.GetInvite().GetExpiresAt(), replay.GetInvite().GetExpiresAt())

	invites, err := guildStore.ListGuildInvites(ctx, store.ListGuildInvitesParams{GuildID: guildID, Limit: 100})
	require.NoError(t, err)
	require.Len(t, invites, 1, "replay must not consume active-invite quota")
}

func TestCreateGuildRoleIdempotentReplayWithPostgres(t *testing.T) {
	guildStore, service := newPostgresGuildService(t)
	ctx := t.Context()
	guildID := createGuildWithPostgres(t, service)

	req := new(guildv1.CreateGuildRoleRequest)
	req.SetGuildId(guildID)
	req.SetActorUserId(1001)
	req.SetName("Moderator")
	req.SetPermissions(PermissionManageMessages)
	req.SetIdempotencyKey("role-intent-1")
	first, err := service.CreateGuildRole(ctx, req)
	require.NoError(t, err)

	replay, err := service.CreateGuildRole(ctx, req)
	require.NoError(t, err)
	require.Equal(t, first.GetRole().GetId(), replay.GetRole().GetId())
	require.Equal(t, first.GetRole().GetPosition(), replay.GetRole().GetPosition())

	roles, err := guildStore.ListGuildRoles(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, roles, 2, "replay must not create another role")

	// A later role computes its position from the persisted state only, so the
	// replay must not have consumed another position.
	second := new(guildv1.CreateGuildRoleRequest)
	second.SetGuildId(guildID)
	second.SetActorUserId(1001)
	second.SetName("Another")
	second.SetPermissions(0)
	second.SetIdempotencyKey("role-intent-2")
	resp, err := service.CreateGuildRole(ctx, second)
	require.NoError(t, err)
	require.Equal(t, first.GetRole().GetPosition()+1, resp.GetRole().GetPosition())
}

func TestCreateGuildChannelFailedCreationRollsBackClaimWithPostgres(t *testing.T) {
	guildStore, service := newPostgresGuildService(t)
	ctx := t.Context()
	guildID := createGuildWithPostgres(t, service)

	// A stale layout revision fails the creation transaction; the idempotency
	// claim must roll back with it so the key can represent a fresh intent.
	req := new(guildv1.CreateGuildChannelRequest)
	req.SetGuildId(guildID)
	req.SetActorUserId(1001)
	req.SetExpectedChannelLayoutRevision(99)
	req.SetName("general")
	req.SetIdempotencyKey("channel-intent-rollback")
	_, err := service.CreateGuildChannel(ctx, req)
	require.Equal(t, codes.Aborted, status.Code(err))
	require.Equal(t, rpcerror.GuildChannelLayoutConflict, rpcerrorReason(t, err))

	// The key is claimable again after the rollback.
	req.SetExpectedChannelLayoutRevision(1)
	created, err := service.CreateGuildChannel(ctx, req)
	require.NoError(t, err)
	require.Equal(t, "general", created.GetChannel().GetName())

	channels, err := guildStore.ListGuildChannels(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, channels, 5, "the retried creation must create exactly one channel")
}

func TestCreateGuildReplaySuppressesEventsWithPostgres(t *testing.T) {
	publisher := new(fakePublisher)
	guildStore, service := newPostgresGuildServiceWithPublisher(t, publisher)
	ctx := t.Context()

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")
	req.SetIdempotencyKey("guild-intent-1")
	first, err := service.CreateGuild(ctx, req)
	require.NoError(t, err)
	require.Len(t, publisher.records, 1, "first creation publishes the guild created event")

	replay, err := service.CreateGuild(ctx, req)
	require.NoError(t, err)
	require.Equal(t, first.GetGuild().GetId(), replay.GetGuild().GetId())
	require.Len(t, publisher.records, 1, "replay must not republish the creation event")

	// A different key is a new intent and publishes again.
	second := new(guildv1.CreateGuildRequest)
	second.SetOwnerId(1001)
	second.SetName("Other")
	second.SetIdempotencyKey("guild-intent-2")
	_, err = service.CreateGuild(ctx, second)
	require.NoError(t, err)
	require.Len(t, publisher.records, 2)
	guilds, err := guildStore.ListUserGuilds(ctx, store.ListUserGuildsParams{UserID: 1001, Limit: 100})
	require.NoError(t, err)
	require.Len(t, guilds, 2)
}

func TestCreateGuildChannelReplaySuppressesEventsWithPostgres(t *testing.T) {
	publisher := new(fakePublisher)
	_, service := newPostgresGuildServiceWithPublisher(t, publisher)
	ctx := t.Context()
	guildID := createGuildWithPostgres(t, service)
	require.Len(t, publisher.records, 1, "guild creation event")

	req := new(guildv1.CreateGuildChannelRequest)
	req.SetGuildId(guildID)
	req.SetActorUserId(1001)
	req.SetExpectedChannelLayoutRevision(1)
	req.SetName("uncat")
	req.SetIdempotencyKey("channel-intent-1")
	created, err := service.CreateGuildChannel(ctx, req)
	require.NoError(t, err)
	eventsAfterCreate := len(publisher.records)
	require.Greater(t, eventsAfterCreate, 1, "creation publishes channel shift, created, and overwrite events")

	replay, err := service.CreateGuildChannel(ctx, req)
	require.NoError(t, err)
	require.Equal(t, created.GetChannel().GetId(), replay.GetChannel().GetId())
	require.Len(t, publisher.records, eventsAfterCreate, "replay must not republish channel or overwrite events")
}

func rpcerrorReason(t *testing.T, err error) string {
	t.Helper()
	info, ok := rpcerror.Parse(err)
	require.True(t, ok)
	return info.Reason
}

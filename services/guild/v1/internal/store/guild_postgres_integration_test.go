//go:build integration

package store

import (
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func testGuildChannelLayoutRevision(t *testing.T, store Store) {
	const guildID, ownerID = int64(19450), int64(29450)
	ctx := t.Context()
	seedGuild(t, store, guildID, ownerID)

	revision, err := store.GetGuildChannelLayoutRevision(ctx, guildID)
	require.NoError(t, err)
	require.Equal(t, int64(1), revision)
	channels, snapshotRevision, err := store.ListGuildChannelsWithRevision(ctx, guildID)
	require.NoError(t, err)
	require.Empty(t, channels)
	require.Equal(t, revision, snapshotRevision)
	batchChannels, batchRevisions, err := store.ListGuildChannelsWithRevisionsByGuilds(ctx, []int64{guildID})
	require.NoError(t, err)
	require.Empty(t, batchChannels)
	require.Equal(t, map[int64]int64{guildID: revision}, batchRevisions)

	revision, err = store.AdvanceGuildChannelLayoutRevision(ctx, guildID, revision)
	require.NoError(t, err)
	require.Equal(t, int64(2), revision)

	_, err = store.AdvanceGuildChannelLayoutRevision(ctx, guildID, 1)
	require.ErrorIs(t, err, ErrGuildChannelLayoutRevisionConflict)

	const rollbackRevision = int64(2)
	sentinel := errors.New("rollback layout revision")
	err = store.Transact(ctx, func(txStore Store) error {
		_, err := txStore.AdvanceGuildChannelLayoutRevision(ctx, guildID, rollbackRevision)
		if err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	revision, err = store.GetGuildChannelLayoutRevision(ctx, guildID)
	require.NoError(t, err)
	require.Equal(t, rollbackRevision, revision)
}

func testGuildAccessRevision(t *testing.T, store Store) {
	const guildID, ownerID, memberID, roleID, channelID = int64(19200), int64(29200), int64(29201), int64(19201), int64(19202)
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)

	revision := func() int64 {
		guild, err := store.GetGuild(ctx, guildID)
		require.NoError(t, err)
		return guild.AccessRevision
	}
	assertAdvanced := func(previous int64) int64 {
		current := revision()
		require.Greater(t, current, previous)
		return current
	}

	current := revision()
	_, err := store.UpdateGuildMemberNickname(ctx, guildID, ownerID, "owner")
	require.NoError(t, err)
	require.Equal(t, current, revision(), "nickname changes do not affect access")

	_, err = store.CreateGuildMember(ctx, guildID, memberID, now)
	require.NoError(t, err)
	current = assertAdvanced(current)

	_, err = store.CreateGuildRole(ctx, roleID, guildID, "reader", 64, 1, now)
	require.NoError(t, err)
	current = assertAdvanced(current)

	_, err = store.UpdateGuildRole(ctx, UpdateGuildRoleParams{
		GuildID: guildID, RoleID: roleID, Name: ptr("renamed"), UpdatedAt: now,
	})
	require.NoError(t, err)
	current = assertAdvanced(current)

	_, err = store.UpdateGuildRole(ctx, UpdateGuildRoleParams{
		GuildID: guildID, RoleID: roleID, Permissions: ptr(uint64(96)), UpdatedAt: now,
	})
	require.NoError(t, err)
	current = assertAdvanced(current)

	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, memberID, roleID, now))
	current = assertAdvanced(current)

	_, err = store.CreateGuildChannel(ctx, channelID, guildID, "general", 1, 0, "", 0, now)
	require.NoError(t, err)
	current = assertAdvanced(current)

	_, err = store.UpdateGuildChannel(ctx, UpdateGuildChannelParams{
		ChannelID: channelID, Name: ptr("chat"), UpdatedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, current, revision(), "channel metadata does not affect access")

	_, err = store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
		ChannelID: channelID, GuildID: guildID, AppliesTo: 2, AppliesToID: memberID,
		Deny: 64, CreatedAt: now,
	})
	require.NoError(t, err)
	current = assertAdvanced(current)

	require.NoError(t, store.DeleteGuildChannelPermissionOverwrite(ctx, channelID, 2, memberID))
	current = assertAdvanced(current)

	_, err = store.TransferGuildOwnership(ctx, guildID, ownerID, memberID)
	require.NoError(t, err)
	assertAdvanced(current)
}

func testResourceQuotas(t *testing.T, store Store) {
	const ownerID = int64(29001)
	ctx := t.Context()
	start := make(chan struct{})
	results := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)

	for i := range 2 {
		guildID := int64(19001 + i)
		go func() {
			ready.Done()
			<-start
			results <- store.Transact(ctx, func(txStore Store) error {
				if err := txStore.CheckResourceQuota(ctx, ResourceQuota{
					Kind: QuotaOwnedGuilds, ScopeID: ownerID, Limit: 1,
				}); err != nil {
					return err
				}
				_, err := txStore.CreateGuild(ctx, guildID, ownerID, "quota", time.Now().UnixMilli())
				return err
			})
		}()
	}
	ready.Wait()
	close(start)

	errs := []error{<-results, <-results}
	var succeeded, exhausted int
	for _, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrResourceLimitExceeded):
			exhausted++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, exhausted)

	const guildID, memberID, channelID = int64(19100), int64(29100), int64(39100)
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, memberID)
	_, err := store.CreateGuildChannel(ctx, channelID, guildID, "general", 1, 0, "", 0, now)
	require.NoError(t, err)
	_, err = store.CreateGuildInvite(ctx, &model.GuildInvite{
		ID: 49100, Code: "quota-active", GuildID: guildID, CreatorUserID: memberID, CreatedAt: now,
	})
	require.NoError(t, err)
	_, err = store.CreateGuildInvite(ctx, &model.GuildInvite{
		ID: 49101, Code: "quota-expired", GuildID: guildID, CreatorUserID: memberID, ExpiresAt: now - 1, CreatedAt: now,
	})
	require.NoError(t, err)
	_, err = store.CreateGuildInvite(ctx, &model.GuildInvite{
		ID: 49102, Code: "quota-exhausted", GuildID: guildID, CreatorUserID: memberID, MaxUses: 1, CreatedAt: now,
	})
	require.NoError(t, err)
	_, err = store.ConsumeGuildInvite(ctx, "quota-exhausted", now)
	require.NoError(t, err)
	_, err = store.UpsertGuildChannelPermissionOverwrite(ctx, &model.ChannelPermissionOverwrite{
		ChannelID: channelID, GuildID: guildID, AppliesTo: 1, AppliesToID: guildID, CreatedAt: now,
	})
	require.NoError(t, err)

	check := func(quota ResourceQuota) error {
		return store.Transact(ctx, func(txStore Store) error {
			return txStore.CheckResourceQuota(ctx, quota)
		})
	}
	require.ErrorIs(t, check(ResourceQuota{Kind: QuotaOwnedGuilds, ScopeID: memberID, Limit: 1}), ErrResourceLimitExceeded)
	require.ErrorIs(t, check(ResourceQuota{Kind: QuotaJoinedGuilds, ScopeID: memberID, Limit: 1}), ErrResourceLimitExceeded)
	require.ErrorIs(t, check(ResourceQuota{
		Kind: QuotaJoinedGuilds, ScopeID: memberID, Limit: 1, TargetID: guildID,
	}), ErrMemberAlreadyExists)
	require.ErrorIs(t, check(ResourceQuota{Kind: QuotaGuildRoles, ScopeID: guildID, Limit: 1}), ErrResourceLimitExceeded)
	require.ErrorIs(t, check(ResourceQuota{Kind: QuotaGuildChannels, ScopeID: guildID, Limit: 1}), ErrResourceLimitExceeded)
	require.ErrorIs(t, check(ResourceQuota{Kind: QuotaActiveInvites, ScopeID: guildID, Limit: 1, Now: now}), ErrResourceLimitExceeded)
	require.NoError(t, check(ResourceQuota{
		Kind: QuotaChannelOverwrites, ScopeID: channelID, Limit: 1, AppliesTo: 1, AppliesToID: guildID,
	}))
	require.ErrorIs(t, check(ResourceQuota{
		Kind: QuotaChannelOverwrites, ScopeID: channelID, Limit: 1, AppliesTo: 2, AppliesToID: memberID,
	}), ErrResourceLimitExceeded)
}

func testGuildCRUD(t *testing.T, store Store) {
	const guildID, ownerID = 10100, 20100
	ctx := t.Context()
	now := time.Now().UnixMilli()

	_, err := store.CreateGuild(ctx, guildID, ownerID, "Cordis", now)
	require.NoError(t, err)
	_, err = store.CreateGuildMember(ctx, guildID, ownerID, now)
	require.NoError(t, err)
	require.NoError(t, store.CreateDefaultRole(ctx, guildID, now))

	g, err := store.GetGuildForMember(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Equal(t, "Cordis", g.Name)
	require.Zero(t, g.IconAssetID)
	require.Equal(t, int64(1), g.Revision)

	gu, err := store.UpdateGuild(ctx, UpdateGuildParams{
		GuildID: guildID, Name: ptr("Updated"), Description: ptr("Community"),
	})
	require.NoError(t, err)
	require.Equal(t, "Updated", gu.Name)
	require.Equal(t, "Community", gu.Description)
	require.Equal(t, int64(2), gu.Revision)

	gu, err = store.UpdateGuildIcon(ctx, guildID, 9001)
	require.NoError(t, err)
	require.Equal(t, int64(9001), gu.IconAssetID)
	require.Equal(t, int64(3), gu.Revision)

	const guildID2 = 10101
	seedGuild(t, store, guildID2, ownerID)
	list, err := store.ListUserGuilds(ctx, ListUserGuildsParams{UserID: ownerID, Limit: 1})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, int64(guildID2), list[0].ID)
	list, err = store.ListUserGuilds(ctx, ListUserGuildsParams{UserID: ownerID, Before: list[0].ID, Limit: 1})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, int64(guildID), list[0].ID)

	dg, err := store.DeleteGuild(ctx, guildID, now)
	require.NoError(t, err)
	require.Equal(t, int64(4), dg.Revision)
	require.True(t, dg.DeletedAt > 0)
	_, err = store.DeleteGuild(ctx, guildID, now)
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = store.GetGuildForMember(ctx, guildID, ownerID)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testTransferGuildOwnership(t *testing.T, store Store) {
	const guildID, ownerID, memberID = 10400, 20400, 20401
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)
	_, err := store.CreateGuildMember(ctx, guildID, memberID, now)
	require.NoError(t, err)

	gu, err := store.TransferGuildOwnership(ctx, guildID, ownerID, memberID)
	require.NoError(t, err)
	require.Equal(t, int64(memberID), gu.OwnerID)
	require.Equal(t, int64(2), gu.Revision)

	_, err = store.TransferGuildOwnership(ctx, guildID, ownerID, memberID)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testGuildDeleteHelpers(t *testing.T, store Store) {
	const guildID, ownerID = 11100, 21100
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)
	_, err := store.CreateGuildRole(ctx, 11101, guildID, "R", 1, 1, now)
	require.NoError(t, err)
	require.NoError(t, store.UpsertGuildMemberProfile(ctx, &model.GuildMemberProfile{
		GuildID: guildID, UserID: ownerID, Username: "owner", ProfileUpdatedAt: 1,
	}))
	require.NoError(t, store.DeleteGuildMemberProfiles(ctx, guildID))
	profiles, err := store.SearchGuildMentionUsers(ctx, SearchGuildMentionUsersParams{GuildID: guildID, Query: "owner", Limit: 10})
	require.NoError(t, err)
	require.Empty(t, profiles)

	require.NoError(t, store.DeleteGuildMembers(ctx, guildID, now))
	_, err = store.GetGuildMember(ctx, guildID, ownerID)
	require.ErrorIs(t, err, sql.ErrNoRows)

	require.NoError(t, store.DeleteGuildRoles(ctx, guildID, now))
	roles, err := store.ListGuildRoles(ctx, guildID)
	require.NoError(t, err)
	require.Empty(t, roles)
}

// seedGuild creates the minimum guild ownership records needed by store tests.

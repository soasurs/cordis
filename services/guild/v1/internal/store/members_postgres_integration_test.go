//go:build integration

package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func testGuildMemberProfileSearch(t *testing.T, store Store) {
	const guildID, ownerID = int64(19700), int64(29700)
	ctx := t.Context()
	now := time.Now().UnixMilli()
	_, err := store.CreateGuild(ctx, guildID, ownerID, "profile-search", now)
	require.NoError(t, err)
	for _, userID := range []int64{ownerID, 29701, 29702} {
		_, err = store.CreateGuildMember(ctx, guildID, userID, now)
		require.NoError(t, err)
	}
	require.NoError(t, store.UpsertGuildMemberProfile(ctx, &model.GuildMemberProfile{
		GuildID: guildID, UserID: ownerID, Username: "alice", Name: "Zed", AvatarAssetID: 77, ProfileUpdatedAt: 10,
	}))
	require.NoError(t, store.UpsertGuildMemberProfile(ctx, &model.GuildMemberProfile{
		GuildID: guildID, UserID: 29701, Username: "bob", Name: "Alice Bob", ProfileUpdatedAt: 10,
	}))
	require.NoError(t, store.UpsertGuildMemberProfile(ctx, &model.GuildMemberProfile{
		GuildID: guildID, UserID: 29702, Username: "carol", Name: "Other", Nickname: "Nick Carol", ProfileUpdatedAt: 10,
	}))

	profiles, err := store.SearchGuildMentionUsers(ctx, SearchGuildMentionUsersParams{
		GuildID: guildID, Query: "ali", Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	require.Equal(t, ownerID, profiles[0].UserID)

	profiles, err = store.SearchGuildMentionUsers(ctx, SearchGuildMentionUsersParams{
		GuildID: guildID, Query: "ali", After: true, AfterMatchRank: 0,
		AfterUsername: "alice", AfterUserID: ownerID, Limit: 10,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{29701}, []int64{profiles[0].UserID})

	profiles, err = store.SearchGuildMentionUsers(ctx, SearchGuildMentionUsersParams{GuildID: guildID, Query: "nick", Limit: 10})
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	require.Equal(t, int64(29702), profiles[0].UserID)
	require.Equal(t, "Nick Carol", profiles[0].Nickname)

	require.NoError(t, store.UpdateGuildMemberProfileNickname(ctx, guildID, ownerID, "Owner Nick"))
	profiles, err = store.SearchGuildMentionUsers(ctx, SearchGuildMentionUsersParams{GuildID: guildID, Query: "owner nick", Limit: 10})
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	require.Equal(t, ownerID, profiles[0].UserID)

	// A stale event cannot overwrite a newer projection.
	require.NoError(t, store.UpsertGuildMemberProfile(ctx, &model.GuildMemberProfile{
		GuildID: guildID, UserID: ownerID, Username: "stale", ProfileUpdatedAt: 9,
	}))
	profiles, err = store.SearchGuildMentionUsers(ctx, SearchGuildMentionUsersParams{GuildID: guildID, Query: "ali", Limit: 10})
	require.NoError(t, err)
	require.Equal(t, ownerID, profiles[0].UserID)

	require.NoError(t, store.UpdateGuildMemberProfilesByUser(ctx, &model.GuildMemberProfile{
		UserID: ownerID, Username: "updated", Name: "Updated", AvatarAssetID: 88, ProfileUpdatedAt: 11,
	}))
	profiles, err = store.SearchGuildMentionUsers(ctx, SearchGuildMentionUsersParams{GuildID: guildID, Query: "updated", Limit: 10})
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	require.Equal(t, ownerID, profiles[0].UserID)

	require.NoError(t, store.UpdateGuildMemberProfilesByUserWithoutAvatar(ctx, &model.GuildMemberProfile{
		UserID: ownerID, Username: "without_avatar", Name: "Without Avatar", ProfileUpdatedAt: 12,
	}))
	profiles, err = store.SearchGuildMentionUsers(ctx, SearchGuildMentionUsersParams{GuildID: guildID, Query: "without_avatar", Limit: 10})
	require.NoError(t, err)
	require.Len(t, profiles, 1)
	require.Equal(t, int64(88), profiles[0].AvatarAssetID)

	keys, err := store.ListGuildMemberProfileKeys(ctx, ListGuildMemberProfileKeysParams{AfterGuildID: guildID - 1, Limit: 10})
	require.NoError(t, err)
	require.Len(t, keys, 3)
}

func testCommonGuildMembership(t *testing.T, store Store) {
	const (
		guildA    = int64(19400)
		guildB    = int64(19401)
		guildC    = int64(19402)
		actor     = int64(29400)
		targetA   = int64(29401)
		targetB   = int64(29402)
		removed   = int64(29403)
		unrelated = int64(29404)
	)
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildA, actor)
	seedGuild(t, store, guildB, actor)
	seedGuild(t, store, guildC, unrelated)
	_, err := store.CreateGuildMember(ctx, guildA, targetA, now)
	require.NoError(t, err)
	_, err = store.CreateGuildMember(ctx, guildB, targetB, now)
	require.NoError(t, err)
	_, err = store.CreateGuildMember(ctx, guildA, removed, now)
	require.NoError(t, err)
	_, err = store.RemoveGuildMember(ctx, guildA, removed, now+1)
	require.NoError(t, err)

	userIDs, err := store.ListUsersWithCommonGuild(ctx, actor, []int64{
		unrelated, removed, targetB, targetA, targetA,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{targetA, targetB}, userIDs)
}

func testGuildMemberLifecycle(t *testing.T, store Store) {
	const guildID, ownerID, memberID2 = 10200, 20200, 20201
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)

	member, err := store.GetGuildMember(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Equal(t, int64(1), member.Revision)

	dup, err := store.CreateGuildMember(ctx, guildID, ownerID, now)
	require.ErrorIs(t, err, ErrMemberAlreadyExists)
	require.Nil(t, dup)

	m2, err := store.CreateGuildMember(ctx, guildID, memberID2, now)
	require.NoError(t, err)
	require.Equal(t, int64(memberID2), m2.UserID)
	require.Equal(t, int64(1), m2.Revision)

	updated, err := store.UpdateGuildMemberNickname(ctx, guildID, ownerID, "Ada")
	require.NoError(t, err)
	require.Equal(t, "Ada", updated.Nickname)
	require.Equal(t, int64(2), updated.Revision)

	members, err := store.ListGuildMembers(ctx, ListGuildMembersParams{GuildID: guildID, Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []int64{memberID2, ownerID}, idsOf(members, func(m *model.GuildMember) int64 { return m.UserID }))

	members, err = store.ListGuildMembers(ctx, ListGuildMembersParams{
		GuildID: guildID, BeforeJoinedAt: members[0].JoinedAt, BeforeUserID: memberID2, Limit: 1,
	})
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, int64(ownerID), members[0].UserID)

	removed, err := store.RemoveGuildMember(ctx, guildID, memberID2, now)
	require.NoError(t, err)
	require.True(t, removed.DeletedAt > 0)
	_, err = store.GetGuildMember(ctx, guildID, memberID2)
	require.ErrorIs(t, err, sql.ErrNoRows)

	rejoined, err := store.CreateGuildMember(ctx, guildID, memberID2, now)
	require.NoError(t, err)
	require.Equal(t, int64(0), rejoined.DeletedAt)
	require.Equal(t, int64(3), rejoined.Revision)
	require.Equal(t, "", rejoined.Nickname)

	_, err = store.UpsertGuildBan(ctx, &model.GuildBan{
		GuildID: guildID, UserID: memberID2, ActorUserID: ownerID,
		Reason: "violation", CreatedAt: now,
	})
	require.NoError(t, err)
	_, err = store.RemoveGuildMember(ctx, guildID, memberID2, now)
	require.NoError(t, err)
	_, err = store.CreateGuildMember(ctx, guildID, memberID2, now)
	require.ErrorIs(t, err, ErrMemberAlreadyExists)
}

//go:build integration

package store

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func testGuildRolesCRUD(t *testing.T, store Store) {
	const guildID, ownerID = 10500, 20500
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)

	roles, err := store.ListGuildRoles(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, int64(guildID), roles[0].ID)
	require.True(t, roles[0].IsDefault)
	require.Equal(t, int32(0), roles[0].Position)
	// VIEW_CHANNEL | SEND_MESSAGES | CREATE_INVITE
	require.Equal(t, uint64(1120), roles[0].Permissions)

	mod, err := store.CreateGuildRole(ctx, 10501, guildID, "Mod", 1024, 5, now)
	require.NoError(t, err)
	require.Equal(t, "Mod", mod.Name)
	require.Equal(t, uint64(1024), mod.Permissions)
	require.Equal(t, int32(5), mod.Position)

	_, err = store.CreateGuildRole(ctx, 10502, guildID, "Admin", 8, 10, now)
	require.NoError(t, err)
	roles, err = store.ListGuildRoles(ctx, guildID)
	require.NoError(t, err)
	require.Equal(t, []int64{10502, 10501, guildID}, idsOf(roles, func(r *model.Role) int64 { return r.ID }))
	roles, err = store.ListGuildRolesByGuilds(ctx, []int64{guildID})
	require.NoError(t, err)
	require.Equal(t, []int64{10502, 10501, guildID}, idsOf(roles, func(r *model.Role) int64 { return r.ID }))

	updated, err := store.UpdateGuildRole(ctx, UpdateGuildRoleParams{
		GuildID: guildID, RoleID: 10501, Name: ptr("Moderator"),
		Permissions: ptr(uint64(2048)), UpdatedAt: now,
	})
	require.NoError(t, err)
	require.Equal(t, "Moderator", updated.Name)
	require.Equal(t, uint64(2048), updated.Permissions)
	require.Equal(t, int64(2), updated.Revision)

	moved, err := store.UpdateGuildRolePosition(ctx, guildID, 10501, 15, now)
	require.NoError(t, err)
	require.Equal(t, int32(15), moved.Position)
	movedRoles, err := store.UpdateGuildRolePositions(ctx, guildID, []int64{10501, 10502}, []int32{6, 7}, now)
	require.NoError(t, err)
	require.Len(t, movedRoles, 2)

	_, err = store.UpdateGuildRolePosition(ctx, guildID, guildID, 100, now)
	require.ErrorIs(t, err, sql.ErrNoRows)

	del, err := store.DeleteGuildRole(ctx, guildID, 10502, now)
	require.NoError(t, err)
	require.True(t, del.DeletedAt > 0)
	_, err = store.GetGuildRole(ctx, guildID, 10502)
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = store.DeleteGuildRole(ctx, guildID, guildID, now)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testGuildMemberRoles(t *testing.T, store Store) {
	const guildID, ownerID, memberID = 10600, 20600, 20601
	ctx := t.Context()
	now := time.Now().UnixMilli()
	seedGuild(t, store, guildID, ownerID)

	_, err := store.CreateGuildRole(ctx, 10601, guildID, "Mod", 1, 1, now)
	require.NoError(t, err)
	_, err = store.CreateGuildRole(ctx, 10602, guildID, "Tmp", 2, 2, now)
	require.NoError(t, err)

	memberRoles, err := store.ListGuildMemberRoles(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)
	require.True(t, memberRoles[0].IsDefault)

	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10601, now))
	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10601, now))
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Equal(t, []int64{10601, guildID}, idsOf(memberRoles, func(r *model.Role) int64 { return r.ID }))

	require.NoError(t, store.RemoveGuildMemberRole(ctx, guildID, ownerID, 10601))
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)

	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10601, now))
	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10602, now))
	require.NoError(t, store.DeleteGuildMemberRoleAssignments(ctx, guildID, ownerID))
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)

	_, err = store.CreateGuildMember(ctx, guildID, memberID, now)
	require.NoError(t, err)
	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, memberID, 10601, now))
	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10601, now))
	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10602, now))
	roleMembers, err := store.ListGuildRoleMembers(ctx, ListGuildRoleMembersParams{
		GuildID: guildID, RoleID: 10601, Limit: 10,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{memberID, ownerID}, idsOf(roleMembers, func(m *model.GuildMember) int64 { return m.UserID }))
	roleMembers, err = store.ListGuildRoleMembers(ctx, ListGuildRoleMembersParams{
		GuildID: guildID, RoleID: 10601,
		BeforeJoinedAt: roleMembers[0].JoinedAt, BeforeUserID: memberID, Limit: 1,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{ownerID}, idsOf(roleMembers, func(m *model.GuildMember) int64 { return m.UserID }))
	roleMembers, err = store.ListGuildRoleMembers(ctx, ListGuildRoleMembersParams{
		GuildID: guildID, RoleID: guildID, Limit: 10,
	})
	require.NoError(t, err)
	require.Equal(t, []int64{memberID, ownerID}, idsOf(roleMembers, func(m *model.GuildMember) int64 { return m.UserID }))
	require.NoError(t, store.DeleteGuildRoleAssignments(ctx, guildID, 10601))
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 2)
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, memberID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)
	memberRoles, err = store.ListGuildMemberRolesByGuilds(ctx, []int64{guildID}, memberID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)

	require.NoError(t, store.AddGuildMemberRole(ctx, guildID, ownerID, 10601, now))
	require.NoError(t, store.DeleteAllGuildRoleAssignments(ctx, guildID))
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, ownerID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)
	memberRoles, err = store.ListGuildMemberRoles(ctx, guildID, memberID)
	require.NoError(t, err)
	require.Len(t, memberRoles, 1)
}

package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func TestMemberAuthorityCombinesRolesAndExpandsAdministrator(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.roles[10][20] = testRole(20, 10, "manager", PermissionManageGuild, 2)
	fakeStore.roles[10][21] = testRole(21, 10, "admin", PermissionAdministrator, 3)
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 20, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 21, 1))

	authority, err := loadMemberAuthority(t.Context(), fakeStore, 10, 1002)
	require.NoError(t, err)
	require.Equal(t, AllGuildPermissions, authority.Permissions)
	require.Equal(t, int32(3), authority.HighestRole)
	require.False(t, authority.IsOwner)
}

func TestCreateGuildRoleRequiresPermissionAndPublishesStringPermissions(t *testing.T) {
	fakeStore := roleTestStore()
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.CreateGuildRoleRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1002)
	req.SetName("moderator")
	req.SetPermissions(PermissionKickMembers)
	_, err := server.CreateGuildRole(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	req.SetActorUserId(1001)
	resp, err := server.CreateGuildRole(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, "moderator", resp.GetRole().GetName())
	require.Equal(t, PermissionKickMembers, resp.GetRole().GetPermissions())
	require.Equal(t, int32(1), resp.GetRole().GetPosition())

	var envelope eventEnvelope[guildRolePayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeGuildRoleCreated, envelope.Type)
	require.Equal(t, "16", envelope.Data.Permissions)
}

func TestManageRolesEnforcesRoleAndMemberHierarchy(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.roles[10][20] = testRole(20, 10, "manager", PermissionManageRoles, 5)
	fakeStore.roles[10][21] = testRole(21, 10, "member", 0, 2)
	fakeStore.roles[10][22] = testRole(22, 10, "peer", 0, 5)
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 20, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1003, 22, 1))
	server := newTestGuildServer(t, fakeStore, new(fakePublisher))

	add := new(guildv1.AddGuildMemberRoleRequest)
	add.SetGuildId(10)
	add.SetActorUserId(1002)
	add.SetUserId(1004)
	add.SetRoleId(21)
	resp, err := server.AddGuildMemberRole(t.Context(), add)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.True(t, fakeStore.memberRoles[10][1004][21])

	add.SetUserId(1003)
	_, err = server.AddGuildMemberRole(t.Context(), add)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	add.SetUserId(1004)
	add.SetRoleId(22)
	_, err = server.AddGuildMemberRole(t.Context(), add)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestAddGuildRoleMembersAssignsManyAndRejectsPartialFailure(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.roles[10][20] = testRole(20, 10, "manager", PermissionManageRoles, 5)
	fakeStore.roles[10][21] = testRole(21, 10, "member", 0, 2)
	fakeStore.roles[10][22] = testRole(22, 10, "peer", 0, 5)
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 20, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1003, 22, 1))
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.AddGuildRoleMembersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1002)
	req.SetRoleId(21)
	req.SetUserIds([]int64{1001, 1004})
	_, err := server.AddGuildRoleMembers(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.False(t, fakeStore.memberRoles[10][1004][21])
	require.Empty(t, publisher.records)

	req.SetUserIds([]int64{1004, 1004})
	resp, err := server.AddGuildRoleMembers(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.True(t, fakeStore.memberRoles[10][1004][21])
	require.Len(t, publisher.records, 1)

	remove := new(guildv1.RemoveGuildRoleMembersRequest)
	remove.SetGuildId(10)
	remove.SetActorUserId(1002)
	remove.SetRoleId(21)
	remove.SetUserIds([]int64{1004})
	removeResp, err := server.RemoveGuildRoleMembers(t.Context(), remove)
	require.NoError(t, err)
	require.True(t, removeResp.GetOk())
	require.False(t, fakeStore.memberRoles[10][1004][21])
}

func TestAddGuildRoleMembersValidation(t *testing.T) {
	server := newTestGuildServer(t, roleTestStore(), nil)
	req := new(guildv1.AddGuildRoleMembersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetRoleId(10)
	_, err := server.AddGuildRoleMembers(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetUserIds(make([]int64, maxGuildRoleMemberBatch+1))
	_, err = server.AddGuildRoleMembers(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetUserIds([]int64{1002})
	_, err = server.AddGuildRoleMembers(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "default role is assigned implicitly")
}

func TestManageRolesCannotGrantPermissionsActorDoesNotHold(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.roles[10][20] = testRole(20, 10, "manager", PermissionManageRoles, 5)
	fakeStore.roles[10][21] = testRole(21, 10, "admin", PermissionAdministrator, 2)
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 20, 1))
	server := newTestGuildServer(t, fakeStore, nil)

	create := new(guildv1.CreateGuildRoleRequest)
	create.SetGuildId(10)
	create.SetActorUserId(1002)
	create.SetName("moderator")
	create.SetPermissions(PermissionKickMembers)
	_, err := server.CreateGuildRole(t.Context(), create)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	add := new(guildv1.AddGuildMemberRoleRequest)
	add.SetGuildId(10)
	add.SetActorUserId(1002)
	add.SetUserId(1004)
	add.SetRoleId(21)
	_, err = server.AddGuildMemberRole(t.Context(), add)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestListGuildRoleMembersUsesCursorAndIncludesDefaultRoleMembers(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.roles[10][20] = testRole(20, 10, "moderator", 0, 1)
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1001, 20, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 20, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1003, 20, 1))
	server := newTestGuildServer(t, fakeStore, nil)

	codec := testCursorCodec(t)
	token, err := codec.Encode(cursor.KindGuildRoleMembers, guildRoleTimeIDPayload{GuildID: 10, RoleID: 20, Time: 1, ID: 1003})
	require.NoError(t, err)
	req := new(guildv1.ListGuildRoleMembersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetRoleId(20)
	req.SetCursor(token)
	req.SetLimit(1)
	resp, err := server.ListGuildRoleMembers(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetMembers(), 1)
	require.Equal(t, int64(1002), resp.GetMembers()[0].GetUserId())
	next, err := codec.Encode(cursor.KindGuildRoleMembers, guildRoleTimeIDPayload{GuildID: 10, RoleID: 20, Time: 1, ID: 1002})
	require.NoError(t, err)
	require.Equal(t, next, resp.GetNextCursor())

	req.SetRoleId(10)
	req.ClearCursor()
	req.SetLimit(2)
	resp, err = server.ListGuildRoleMembers(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, []int64{1004, 1003}, []int64{
		resp.GetMembers()[0].GetUserId(), resp.GetMembers()[1].GetUserId(),
	})
	require.True(t, resp.HasNextCursor())
}

func TestListGuildRoleMembersPagesWithServerCursors(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.roles[10][20] = testRole(20, 10, "moderator", 0, 1)
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1001, 20, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 20, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1003, 20, 1))
	server := newTestGuildServer(t, fakeStore, nil)

	seen := make([]int64, 0, 3)
	req := new(guildv1.ListGuildRoleMembersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetRoleId(20)
	req.SetLimit(1)
	for {
		resp, err := server.ListGuildRoleMembers(t.Context(), req)
		require.NoError(t, err)
		require.Len(t, resp.GetMembers(), 1)
		seen = append(seen, resp.GetMembers()[0].GetUserId())
		if !resp.HasNextCursor() {
			break
		}
		req.SetCursor(resp.GetNextCursor())
	}
	require.Equal(t, []int64{1003, 1002, 1001}, seen)
}

func TestListGuildRoleMembersRejectsBadCursors(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.roles[10][20] = testRole(20, 10, "moderator", 0, 1)
	server := newTestGuildServer(t, fakeStore, nil)

	assertRejectsBadCursors(t, cursor.KindGuildRoleMembers, guildRoleTimeIDPayload{GuildID: 10, RoleID: 20, Time: 1, ID: 1001}, func(token string) error {
		req := new(guildv1.ListGuildRoleMembersRequest)
		req.SetGuildId(10)
		req.SetActorUserId(1001)
		req.SetRoleId(20)
		req.SetCursor(token)
		_, err := server.ListGuildRoleMembers(t.Context(), req)
		return err
	})
}

func TestListGuildRoleMembersRejectsCrossRoleCursor(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.roles[10][20] = testRole(20, 10, "moderator", 0, 1)
	fakeStore.roles[10][21] = testRole(21, 10, "other", 0, 2)
	server := newTestGuildServer(t, fakeStore, nil)

	codec := testCursorCodec(t)
	token, err := codec.Encode(cursor.KindGuildRoleMembers, guildRoleTimeIDPayload{
		GuildID: 10, RoleID: 21, Time: 1, ID: 1001,
	})
	require.NoError(t, err)
	req := new(guildv1.ListGuildRoleMembersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetRoleId(20)
	req.SetCursor(token)
	_, err = server.ListGuildRoleMembers(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestListGuildRoleMembersRejectsUnknownRole(t *testing.T) {
	server := newTestGuildServer(t, roleTestStore(), nil)
	req := new(guildv1.ListGuildRoleMembersRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetRoleId(99)

	_, err := server.ListGuildRoleMembers(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestReorderGuildRolesRejectsPositionCollision(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.roles[10][20] = testRole(20, 10, "one", 0, 1)
	fakeStore.roles[10][21] = testRole(21, 10, "two", 0, 2)
	server := newTestGuildServer(t, fakeStore, nil)

	position := new(guildv1.GuildRolePosition)
	position.SetRoleId(20)
	position.SetPosition(2)
	req := new(guildv1.ReorderGuildRolesRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetPositions([]*guildv1.GuildRolePosition{position})
	_, err := server.ReorderGuildRoles(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGuildAndKickPermissionsUseRoleHierarchy(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.roles[10][20] = testRole(
		20,
		10,
		"moderator",
		PermissionManageGuild|PermissionKickMembers,
		5,
	)
	fakeStore.roles[10][21] = testRole(21, 10, "member", 0, 2)
	fakeStore.roles[10][22] = testRole(22, 10, "higher", 0, 6)
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1002, 20, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1003, 21, 1))
	require.NoError(t, fakeStore.AddGuildMemberRole(t.Context(), 10, 1004, 22, 1))
	server := newTestGuildServer(t, fakeStore, new(fakePublisher))

	update := new(guildv1.UpdateGuildRequest)
	update.SetGuildId(10)
	update.SetActorUserId(1002)
	update.SetName("renamed")
	updated, err := server.UpdateGuild(t.Context(), update)
	require.NoError(t, err)
	require.Equal(t, "renamed", updated.GetGuild().GetName())

	kick := new(guildv1.KickGuildMemberRequest)
	kick.SetGuildId(10)
	kick.SetActorUserId(1002)
	kick.SetUserId(1003)
	_, err = server.KickGuildMember(t.Context(), kick)
	require.NoError(t, err)

	kick.SetUserId(1004)
	_, err = server.KickGuildMember(t.Context(), kick)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestDeleteDefaultGuildRoleRejected(t *testing.T) {
	fakeStore := roleTestStore()
	server := newTestGuildServer(t, fakeStore, new(fakePublisher))

	req := new(guildv1.DeleteGuildRoleRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetRoleId(10)
	_, err := server.DeleteGuildRole(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "default role cannot be deleted")
	role, err := fakeStore.GetGuildRole(t.Context(), 10, 10)
	require.NoError(t, err)
	require.True(t, role.IsDefault)
	require.Zero(t, role.DeletedAt)
}

func roleTestStore() *fakeStore {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002, 1003, 1004)
	_ = fakeStore.CreateDefaultRole(context.Background(), 10, 1)
	return fakeStore
}

func testRole(id, guildID int64, name string, permissions uint64, position int32) *model.Role {
	return &model.Role{
		ID: id, GuildID: guildID, Name: name, Permissions: permissions,
		Position: position, Revision: 1, CreatedAt: 1,
	}
}

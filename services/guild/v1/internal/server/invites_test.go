package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func TestCreateGuildInviteRequiresPermission(t *testing.T) {
	fakeStore := roleTestStore()
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.CreateGuildInviteRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1002)
	req.SetMaxUses(5)
	req.SetExpiresInMs(60_000)
	resp, err := server.CreateGuildInvite(t.Context(), req)
	require.NoError(t, err)
	invite := resp.GetInvite()
	require.Len(t, invite.GetCode(), inviteCodeLength)
	require.Equal(t, int64(10), invite.GetGuildId())
	require.Equal(t, int64(1002), invite.GetCreatorUserId())
	require.Equal(t, int32(5), invite.GetMaxUses())
	require.Equal(t, int32(0), invite.GetUses())
	require.Equal(t, invite.GetCreatedAt()+60_000, invite.GetExpiresAt())

	// Without a TTL the invite never expires.
	req.SetExpiresInMs(0)
	resp, err = server.CreateGuildInvite(t.Context(), req)
	require.NoError(t, err)
	require.Zero(t, resp.GetInvite().GetExpiresAt())

	// Revoking CREATE_INVITE from @everyone blocks plain members.
	fakeStore.roles[10][10].Permissions = PermissionViewChannel | PermissionSendMessages
	_, err = server.CreateGuildInvite(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	// The owner bypasses role permissions.
	req.SetActorUserId(1001)
	_, err = server.CreateGuildInvite(t.Context(), req)
	require.NoError(t, err)

	// Non-members cannot see the guild at all.
	req.SetActorUserId(9999)
	_, err = server.CreateGuildInvite(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestCreateGuildInviteValidation(t *testing.T) {
	server := newTestGuildServer(t, roleTestStore(), nil)

	req := new(guildv1.CreateGuildInviteRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetMaxUses(-1)
	_, err := server.CreateGuildInvite(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetMaxUses(maxInviteMaxUses + 1)
	_, err = server.CreateGuildInvite(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))

	req.SetMaxUses(0)
	req.SetExpiresInMs(-1)
	_, err = server.CreateGuildInvite(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGetGuildInvitePreview(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.invites["preview-ok"] = &model.GuildInvite{
		ID: 501, Code: "preview-ok", GuildID: 10, CreatorUserID: 1001, CreatedAt: 1,
	}
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.GetGuildInviteRequest)
	req.SetCode("preview-ok")
	resp, err := server.GetGuildInvite(t.Context(), req)
	require.NoError(t, err)
	preview := resp.GetPreview()
	require.Equal(t, "preview-ok", preview.GetCode())
	require.Equal(t, int64(10), preview.GetGuildId())
	require.Equal(t, "Guild", preview.GetGuildName())
	require.Equal(t, int64(4), preview.GetMemberCount())

	req.SetCode("unknown")
	_, err = server.GetGuildInvite(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))

	fakeStore.invites["expired"] = &model.GuildInvite{
		ID: 502, Code: "expired", GuildID: 10, CreatorUserID: 1001,
		ExpiresAt: time.Now().UnixMilli() - 1, CreatedAt: 1,
	}
	req.SetCode("expired")
	_, err = server.GetGuildInvite(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))

	fakeStore.invites["exhausted"] = &model.GuildInvite{
		ID: 503, Code: "exhausted", GuildID: 10, CreatorUserID: 1001,
		MaxUses: 1, Uses: 1, CreatedAt: 1,
	}
	req.SetCode("exhausted")
	_, err = server.GetGuildInvite(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))

	req.SetCode("   ")
	_, err = server.GetGuildInvite(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestListGuildInvitesRequiresManageGuild(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.invites["list-a"] = &model.GuildInvite{ID: 601, Code: "list-a", GuildID: 10, CreatorUserID: 1001, CreatedAt: 1}
	fakeStore.invites["list-b"] = &model.GuildInvite{ID: 602, Code: "list-b", GuildID: 10, CreatorUserID: 1002, CreatedAt: 2}
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.ListGuildInvitesRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	resp, err := server.ListGuildInvites(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetInvites(), 2)
	require.Equal(t, int64(602), resp.GetInvites()[0].GetId())
	require.Equal(t, int64(601), resp.GetBeforeId())

	req.SetBeforeId(602)
	req.SetLimit(1)
	resp, err = server.ListGuildInvites(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetInvites(), 1)
	require.Equal(t, int64(601), resp.GetInvites()[0].GetId())

	// A plain member holds CREATE_INVITE but not MANAGE_GUILD.
	req.SetActorUserId(1002)
	_, err = server.ListGuildInvites(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestDeleteGuildInviteCreatorAndManager(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.invites["mine"] = &model.GuildInvite{ID: 701, Code: "mine", GuildID: 10, CreatorUserID: 1002, CreatedAt: 1}
	fakeStore.invites["other"] = &model.GuildInvite{ID: 702, Code: "other", GuildID: 10, CreatorUserID: 1003, CreatedAt: 1}
	server := newTestGuildServer(t, fakeStore, nil)

	// The creator may delete their own invite without MANAGE_GUILD.
	req := new(guildv1.DeleteGuildInviteRequest)
	req.SetCode("mine")
	req.SetActorUserId(1002)
	resp, err := server.DeleteGuildInvite(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.Nil(t, fakeStore.invites["mine"])

	// A plain member cannot delete someone else's invite.
	req.SetCode("other")
	req.SetActorUserId(1002)
	_, err = server.DeleteGuildInvite(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	// The owner can delete any invite.
	req.SetActorUserId(1001)
	_, err = server.DeleteGuildInvite(t.Context(), req)
	require.NoError(t, err)

	req.SetCode("unknown")
	_, err = server.DeleteGuildInvite(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestJoinGuildByInviteCreatesMemberAndPublishesEvent(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.invites["join-me"] = &model.GuildInvite{
		ID: 801, Code: "join-me", GuildID: 10, CreatorUserID: 1001, MaxUses: 2, CreatedAt: 1,
	}
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.JoinGuildByInviteRequest)
	req.SetCode("join-me")
	req.SetUserId(2001)
	resp, err := server.JoinGuildByInvite(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int64(10), resp.GetGuild().GetId())
	require.Equal(t, int64(2001), resp.GetMember().GetUserId())
	require.Equal(t, int32(1), fakeStore.invites["join-me"].Uses)

	var envelope eventEnvelope[guildMemberPayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeGuildMemberJoined, envelope.Type)
	require.Equal(t, "10", envelope.Data.GuildID)
	require.Equal(t, "2001", envelope.Data.UserID)

	// An active member cannot join twice.
	req.SetUserId(1002)
	_, err = server.JoinGuildByInvite(t.Context(), req)
	require.Equal(t, codes.AlreadyExists, status.Code(err))

	// The duplicate join does not consume a use; the next new member takes the
	// final use and only the following join observes exhaustion.
	req.SetUserId(2003)
	_, err = server.JoinGuildByInvite(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, int32(2), fakeStore.invites["join-me"].Uses)

	req.SetUserId(2004)
	_, err = server.JoinGuildByInvite(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestJoinGuildByInviteRejectsBannedAndDeletedGuild(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.invites["banned"] = &model.GuildInvite{ID: 802, Code: "banned", GuildID: 10, CreatorUserID: 1001, CreatedAt: 1}
	fakeStore.bans[10] = map[int64]*model.GuildBan{
		2002: {GuildID: 10, UserID: 2002, ActorUserID: 1001, CreatedAt: 1},
	}
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.JoinGuildByInviteRequest)
	req.SetCode("banned")
	req.SetUserId(2002)
	_, err := server.JoinGuildByInvite(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	// A soft-deleted guild hides its invites.
	fakeStore.guilds[10].DeletedAt = 5
	req.SetUserId(2004)
	_, err = server.JoinGuildByInvite(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))

	req.SetCode("")
	_, err = server.JoinGuildByInvite(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

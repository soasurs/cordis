package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

func TestBanGuildMemberRemovesMemberAndBlocksRejoin(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.profiles[10] = map[int64]*model.GuildMemberProfile{
		1002: {GuildID: 10, UserID: 1002, Username: "user_1002"},
	}
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	banReq := new(guildv1.BanGuildMemberRequest)
	banReq.SetGuildId(10)
	banReq.SetActorUserId(1001)
	banReq.SetUserId(1002)
	banReq.SetReason("spam")
	resp, err := server.BanGuildMember(t.Context(), banReq)
	require.NoError(t, err)
	require.Equal(t, "spam", resp.GetBan().GetReason())
	require.NotZero(t, fakeStore.members[10][1002].DeletedAt)
	require.NotContains(t, fakeStore.profiles[10], int64(1002))
	var envelope eventEnvelope[guildMemberBannedPayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, "1002", envelope.Data.Profile.UserID)
	require.Equal(t, "1001", envelope.Data.ActorProfile.UserID)

	addReq := new(guildv1.AddGuildMemberRequest)
	addReq.SetGuildId(10)
	addReq.SetActorUserId(1001)
	addReq.SetUserId(1002)
	_, err = server.AddGuildMember(t.Context(), addReq)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestListAndUnbanGuildMember(t *testing.T) {
	fakeStore := roleTestStore()
	server := newTestGuildServer(t, fakeStore, nil)

	banReq := new(guildv1.BanGuildMemberRequest)
	banReq.SetGuildId(10)
	banReq.SetActorUserId(1001)
	banReq.SetUserId(1002)
	_, err := server.BanGuildMember(t.Context(), banReq)
	require.NoError(t, err)

	listReq := new(guildv1.ListGuildBansRequest)
	listReq.SetGuildId(10)
	listReq.SetActorUserId(1001)
	listResp, err := server.ListGuildBans(t.Context(), listReq)
	require.NoError(t, err)
	require.Len(t, listResp.GetBans(), 1)
	require.Equal(t, int64(1002), listResp.GetBans()[0].GetUserId())
	require.False(t, listResp.HasNextCursor())

	unbanReq := new(guildv1.UnbanGuildMemberRequest)
	unbanReq.SetGuildId(10)
	unbanReq.SetActorUserId(1001)
	unbanReq.SetUserId(1002)
	unbanResp, err := server.UnbanGuildMember(t.Context(), unbanReq)
	require.NoError(t, err)
	require.True(t, unbanResp.GetOk())
	require.Empty(t, fakeStore.bans[10])
}

func TestListGuildBansPagesWithServerCursors(t *testing.T) {
	fakeStore := roleTestStore()
	fakeStore.bans[10] = map[int64]*model.GuildBan{
		2001: {GuildID: 10, UserID: 2001, ActorUserID: 1001, CreatedAt: 1},
		2002: {GuildID: 10, UserID: 2002, ActorUserID: 1001, CreatedAt: 1},
		2003: {GuildID: 10, UserID: 2003, ActorUserID: 1001, CreatedAt: 1},
	}
	server := newTestGuildServer(t, fakeStore, nil)

	seen := make([]int64, 0, 3)
	req := new(guildv1.ListGuildBansRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetLimit(1)
	for {
		resp, err := server.ListGuildBans(t.Context(), req)
		require.NoError(t, err)
		require.Len(t, resp.GetBans(), 1)
		seen = append(seen, resp.GetBans()[0].GetUserId())
		if !resp.HasNextCursor() {
			break
		}
		req.SetCursor(resp.GetNextCursor())
	}
	require.Equal(t, []int64{2003, 2002, 2001}, seen)
}

func TestListGuildBansRejectsBadCursors(t *testing.T) {
	fakeStore := roleTestStore()
	server := newTestGuildServer(t, fakeStore, nil)

	assertRejectsBadCursors(t, cursor.KindGuildBans, guildTimeIDPayload{GuildID: 10, Time: 1, ID: 2001}, func(token string) error {
		req := new(guildv1.ListGuildBansRequest)
		req.SetGuildId(10)
		req.SetActorUserId(1001)
		req.SetCursor(token)
		_, err := server.ListGuildBans(t.Context(), req)
		return err
	})
}

func TestListGuildBansRejectsCrossGuildCursor(t *testing.T) {
	fakeStore := roleTestStore()
	server := newTestGuildServer(t, fakeStore, nil)

	codec := testCursorCodec(t)
	token, err := codec.Encode(cursor.KindGuildBans, guildTimeIDPayload{GuildID: 99, Time: 1, ID: 2001})
	require.NoError(t, err)
	req := new(guildv1.ListGuildBansRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetCursor(token)
	_, err = server.ListGuildBans(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

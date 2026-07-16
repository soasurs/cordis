package server

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
)

func TestBanGuildMemberRemovesMemberAndBlocksRejoin(t *testing.T) {
	fakeStore := roleTestStore()
	server := newTestGuildServer(t, fakeStore, nil)

	banReq := new(guildv1.BanGuildMemberRequest)
	banReq.SetGuildId(10)
	banReq.SetActorUserId(1001)
	banReq.SetUserId(1002)
	banReq.SetReason("spam")
	resp, err := server.BanGuildMember(t.Context(), banReq)
	require.NoError(t, err)
	require.Equal(t, "spam", resp.GetBan().GetReason())
	require.NotZero(t, fakeStore.members[10][1002].DeletedAt)

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
	require.Equal(t, int64(1002), listResp.GetBeforeUserId())

	unbanReq := new(guildv1.UnbanGuildMemberRequest)
	unbanReq.SetGuildId(10)
	unbanReq.SetActorUserId(1001)
	unbanReq.SetUserId(1002)
	unbanResp, err := server.UnbanGuildMember(t.Context(), unbanReq)
	require.NoError(t, err)
	require.True(t, unbanResp.GetOk())
	require.Empty(t, fakeStore.bans[10])
}

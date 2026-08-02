package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
)

func TestCreateDmChannelUsesAuthenticatedUser(t *testing.T) {
	channel := new(messagev1.DmChannel)
	channel.SetId(500)
	channel.SetUserLo(1001)
	channel.SetUserHi(2002)
	channel.SetCreatedAt(4001)
	svcResp := new(messagev1.CreateDmChannelResponse)
	svcResp.SetChannel(channel)

	messageClient := &fakeMessageClient{createDmChannelResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.CreateDmChannelRequest)
	req.SetTargetId(2002)
	resp, err := client.CreateDmChannel(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), messageClient.createDmChannelRequest.GetUserId())
	require.Equal(t, int64(2002), messageClient.createDmChannelRequest.GetTargetId())
	// The stored pair is translated into the caller's perspective.
	require.Equal(t, int64(2002), resp.GetChannel().GetRecipientId())
	require.Equal(t, int64(2002), resp.GetChannel().GetRecipient().GetUserId())
	require.Equal(t, int64(500), resp.GetChannel().GetId())
}

func TestListDmChannelsMapsPerspective(t *testing.T) {
	channel := new(messagev1.DmChannel)
	channel.SetId(500)
	channel.SetUserLo(42)
	channel.SetUserHi(1001)
	svcResp := new(messagev1.ListDmChannelsResponse)
	svcResp.SetChannels([]*messagev1.DmChannel{channel})
	svcResp.SetNextCursor("cursor-token")

	messageClient := &fakeMessageClient{listDmChannelsResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.ListDmChannelsRequest)
	req.SetCursor("cursor-token")
	req.SetLimit(10)
	resp, err := client.ListDmChannels(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), messageClient.listDmChannelsRequest.GetUserId())
	require.Equal(t, "cursor-token", messageClient.listDmChannelsRequest.GetCursor())
	require.Len(t, resp.GetChannels(), 1)
	// The caller is user_hi here, so the recipient is user_lo.
	require.Equal(t, int64(42), resp.GetChannels()[0].GetRecipientId())
	require.Equal(t, int64(42), resp.GetChannels()[0].GetRecipient().GetUserId())
	require.Equal(t, "cursor-token", resp.GetNextCursor())
}

func TestAckMessageUsesAuthenticatedUser(t *testing.T) {
	ackRes := new(messagev1.AckMessageResponse)
	readState := new(messagev1.ChannelReadState)
	readState.SetChannelId(2001)
	readState.SetLastMessageId(3002)
	readState.SetLastReadMessageId(3001)
	readState.SetMentionCount(2)
	ackRes.SetReadState(readState)
	messageClient := &fakeMessageClient{ackMessageResponse: ackRes}
	client, closeServer := newMessageHTTPClient(t, &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.AckMessageRequest)
	req.SetChannelId(2001)
	req.SetMessageId(3001)
	resp, err := client.AckMessage(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(2001), resp.GetReadState().GetChannelId())
	require.Equal(t, int64(3002), resp.GetReadState().GetLastMessageId())
	require.Equal(t, int64(3001), resp.GetReadState().GetLastReadMessageId())
	require.Equal(t, int32(2), resp.GetReadState().GetMentionCount())
	require.Equal(t, int64(1001), messageClient.ackMessageRequest.GetUserId())
	require.Equal(t, int64(2001), messageClient.ackMessageRequest.GetChannelId())
	require.Equal(t, int64(3001), messageClient.ackMessageRequest.GetMessageId())
}

func TestGetReadStatesUsesAuthenticatedScopedRequest(t *testing.T) {
	dm := new(messagev1.DmChannel)
	dm.SetId(500)
	dm.SetUserLo(1001)
	dm.SetUserHi(2002)
	state := new(messagev1.ChannelReadState)
	state.SetChannelId(500)
	state.SetLastMessageId(600)
	svcResp := new(messagev1.GetReadStatesResponse)
	svcResp.SetDmChannels([]*messagev1.DmChannel{dm})
	svcResp.SetReadStates([]*messagev1.ChannelReadState{state})
	messageClient := &fakeMessageClient{getReadStatesResponse: svcResp}
	client, closeServer := newMessageHTTPClient(t, &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}, messageClient, "access-token")
	defer closeServer()

	req := new(apiv1.GetReadStatesRequest)
	req.SetScope(apiv1.ReadStateScopeType_READ_STATE_SCOPE_TYPE_ALL_DMS)
	resp, err := client.GetReadStates(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, int64(1001), messageClient.getReadStatesRequest.GetUserId())
	require.Equal(t, messagev1.ReadStateScopeType_READ_STATE_SCOPE_TYPE_ALL_DMS, messageClient.getReadStatesRequest.GetScope())
	require.Equal(t, int64(2002), resp.GetDmChannels()[0].GetRecipientId())
	require.Equal(t, int64(2002), resp.GetDmChannels()[0].GetRecipient().GetUserId())
	require.Equal(t, int64(600), resp.GetReadStates()[0].GetLastMessageId())
}

package server

import (
	"context"

	"google.golang.org/grpc"

	authenticatorv1 "github.com/soasurs/cordis/gen/authenticator/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
)

type fakeAuthenticator struct {
	authenticatorv1.AuthenticatorServiceClient
}

type rejectedGatewayTicketAuthenticator struct {
	authenticatorv1.AuthenticatorServiceClient
}

func (rejectedGatewayTicketAuthenticator) RedeemGatewayTicket(
	context.Context,
	*authenticatorv1.RedeemGatewayTicketRequest,
	...grpc.CallOption,
) (*authenticatorv1.RedeemGatewayTicketResponse, error) {
	return new(authenticatorv1.RedeemGatewayTicketResponse), nil
}

func (fakeAuthenticator) VerifyAccessToken(
	context.Context,
	*authenticatorv1.VerifyAccessTokenRequest,
	...grpc.CallOption,
) (*authenticatorv1.VerifyAccessTokenResponse, error) {
	resp := new(authenticatorv1.VerifyAccessTokenResponse)
	resp.SetOk(true)
	resp.SetUserId(1001)
	resp.SetSessionId(2002)
	resp.SetExpiresAt(3003)
	return resp, nil
}

func (fakeAuthenticator) RedeemGatewayTicket(
	context.Context,
	*authenticatorv1.RedeemGatewayTicketRequest,
	...grpc.CallOption,
) (*authenticatorv1.RedeemGatewayTicketResponse, error) {
	resp := new(authenticatorv1.RedeemGatewayTicketResponse)
	resp.SetOk(true)
	resp.SetUserId(1001)
	resp.SetSessionId(2002)
	resp.SetAccessTokenExpiresAt(3003)
	return resp, nil
}

type fakeGuild struct {
	guildv1.GuildServiceClient
}

func (fakeGuild) GetUserReadyState(
	context.Context,
	*guildv1.GetUserReadyStateRequest,
	...grpc.CallOption,
) (*guildv1.GetUserReadyStateResponse, error) {
	return new(guildv1.GetUserReadyStateResponse), nil
}

func (fakeGuild) AuthorizeGuildChannel(
	context.Context,
	*guildv1.AuthorizeGuildChannelRequest,
	...grpc.CallOption,
) (*guildv1.AuthorizeGuildChannelResponse, error) {
	resp := new(guildv1.AuthorizeGuildChannelResponse)
	resp.SetAllowed(true)
	resp.SetGuildId(9001)
	return resp, nil
}

type fakePresence struct {
	presencev1.PresenceServiceClient
}

func (fakePresence) ResolveUsersPresence(
	_ context.Context,
	req *presencev1.ResolveUsersPresenceRequest,
	_ ...grpc.CallOption,
) (*presencev1.ResolveUsersPresenceResponse, error) {
	presences := make([]*presencev1.UserPresence, 0, len(req.GetUserIds()))
	for _, userID := range req.GetUserIds() {
		presence := new(presencev1.UserPresence)
		presence.SetUserId(userID)
		presence.SetStatus(presencev1.PresenceStatus_PRESENCE_STATUS_OFFLINE)
		presence.SetVersion(userID + 1)
		presences = append(presences, presence)
	}
	resp := new(presencev1.ResolveUsersPresenceResponse)
	resp.SetPresences(presences)
	return resp, nil
}

type fakeMessage struct {
	messagev1.MessageServiceClient
	request  *messagev1.GetUserReadyStateRequest
	response *messagev1.GetUserReadyStateResponse
	started  chan struct{}
	release  chan struct{}
}

func (m *fakeMessage) GetUserReadyState(
	ctx context.Context,
	req *messagev1.GetUserReadyStateRequest,
	_ ...grpc.CallOption,
) (*messagev1.GetUserReadyStateResponse, error) {
	m.request = req
	if m.started != nil {
		close(m.started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-m.release:
		}
	}
	if m.response == nil {
		return new(messagev1.GetUserReadyStateResponse), nil
	}
	return m.response, nil
}

type recordingPresence struct {
	fakePresence
	updates  []*presencev1.UpdateUserPresenceRequest
	requests [][]int64
}

type batchPresence struct {
	fakePresence
	requests []*presencev1.RefreshUserSessionsRequest
}

func (p *batchPresence) RefreshUserSessions(
	_ context.Context,
	req *presencev1.RefreshUserSessionsRequest,
	_ ...grpc.CallOption,
) (*presencev1.RefreshUserSessionsResponse, error) {
	p.requests = append(p.requests, req)
	return new(presencev1.RefreshUserSessionsResponse), nil
}

func (p *recordingPresence) UpdateUserPresence(
	_ context.Context,
	req *presencev1.UpdateUserPresenceRequest,
	_ ...grpc.CallOption,
) (*presencev1.UpdateUserPresenceResponse, error) {
	p.updates = append(p.updates, req)
	return new(presencev1.UpdateUserPresenceResponse), nil
}

func (p *recordingPresence) ResolveUsersPresence(
	ctx context.Context,
	req *presencev1.ResolveUsersPresenceRequest,
	opts ...grpc.CallOption,
) (*presencev1.ResolveUsersPresenceResponse, error) {
	p.requests = append(p.requests, append([]int64(nil), req.GetUserIds()...))
	return p.fakePresence.ResolveUsersPresence(ctx, req, opts...)
}

func (fakePresence) RegisterUserSession(
	context.Context,
	*presencev1.RegisterUserSessionRequest,
	...grpc.CallOption,
) (*presencev1.RegisterUserSessionResponse, error) {
	preference := new(presencev1.UserPresencePreference)
	preference.SetUserId(1001)
	preference.SetStatus(presencev1.PresenceStatus_PRESENCE_STATUS_ONLINE)
	preference.SetVersion(1)
	resp := new(presencev1.RegisterUserSessionResponse)
	resp.SetPreference(preference)
	return resp, nil
}

func (fakePresence) RefreshUserSession(
	context.Context,
	*presencev1.RefreshUserSessionRequest,
	...grpc.CallOption,
) (*presencev1.RefreshUserSessionResponse, error) {
	return new(presencev1.RefreshUserSessionResponse), nil
}

func (fakePresence) RefreshUserSessions(
	context.Context,
	*presencev1.RefreshUserSessionsRequest,
	...grpc.CallOption,
) (*presencev1.RefreshUserSessionsResponse, error) {
	return new(presencev1.RefreshUserSessionsResponse), nil
}

func (fakePresence) UpdateUserPresence(
	context.Context,
	*presencev1.UpdateUserPresenceRequest,
	...grpc.CallOption,
) (*presencev1.UpdateUserPresenceResponse, error) {
	return new(presencev1.UpdateUserPresenceResponse), nil
}

func (fakePresence) RemoveUserSession(
	context.Context,
	*presencev1.RemoveUserSessionRequest,
	...grpc.CallOption,
) (*presencev1.RemoveUserSessionResponse, error) {
	return new(presencev1.RemoveUserSessionResponse), nil
}

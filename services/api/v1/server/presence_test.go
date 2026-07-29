package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

type presenceUserClient struct {
	userv1.UserServiceClient
	request       *userv1.CheckRelationshipsRequest
	relationships []*userv1.Relationship
}

func (c *presenceUserClient) CheckRelationships(
	_ context.Context,
	req *userv1.CheckRelationshipsRequest,
	_ ...grpc.CallOption,
) (*userv1.CheckRelationshipsResponse, error) {
	c.request = req
	resp := new(userv1.CheckRelationshipsResponse)
	resp.SetRelationships(c.relationships)
	return resp, nil
}

type presenceGuildClient struct {
	guildv1.GuildServiceClient
	request *guildv1.FilterUsersWithCommonGuildRequest
	userIDs []int64
}

func (c *presenceGuildClient) FilterUsersWithCommonGuild(
	_ context.Context,
	req *guildv1.FilterUsersWithCommonGuildRequest,
	_ ...grpc.CallOption,
) (*guildv1.FilterUsersWithCommonGuildResponse, error) {
	c.request = req
	resp := new(guildv1.FilterUsersWithCommonGuildResponse)
	resp.SetUserIds(c.userIDs)
	return resp, nil
}

type presenceInternalClient struct {
	presencev1.PresenceServiceClient
	request   *presencev1.ResolveUsersPresenceRequest
	presences []*presencev1.UserPresence
}

func (c *presenceInternalClient) ResolveUsersPresence(
	_ context.Context,
	req *presencev1.ResolveUsersPresenceRequest,
	_ ...grpc.CallOption,
) (*presencev1.ResolveUsersPresenceResponse, error) {
	c.request = req
	resp := new(presencev1.ResolveUsersPresenceResponse)
	resp.SetPresences(c.presences)
	return resp, nil
}

func TestResolveUsersPresenceFiltersVisibilityAndPrivateFields(t *testing.T) {
	userClient := &presenceUserClient{relationships: []*userv1.Relationship{
		relationship(1001, 1002, userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND),
		relationship(1001, 1004, userv1.RelationshipType_RELATIONSHIP_TYPE_BLOCKED),
	}}
	guildClient := &presenceGuildClient{userIDs: []int64{1003, 1004}}
	presenceClient := &presenceInternalClient{presences: []*presencev1.UserPresence{
		internalPresence(1001, presencev1.PresenceStatus_PRESENCE_STATUS_ONLINE, 11),
		internalPresence(1002, presencev1.PresenceStatus_PRESENCE_STATUS_INVISIBLE, 12),
		internalPresence(1003, presencev1.PresenceStatus_PRESENCE_STATUS_IDLE, 13),
		internalPresence(1004, presencev1.PresenceStatus_PRESENCE_STATUS_DND, 14),
	}}
	client, closeServer := newPresenceHTTPClient(t, userClient, guildClient, presenceClient)
	defer closeServer()

	req := new(apiv1.ResolveUsersPresenceRequest)
	req.SetUserIds([]int64{1005, 1004, 1003, 1002, 1001, 1002})
	resp, err := client.ResolveUsersPresence(t.Context(), req)
	require.NoError(t, err)

	require.Equal(t, []int64{1002, 1003, 1004, 1005}, userClient.request.GetTargetIds())
	require.False(t, userClient.request.GetIncludeReverse())
	require.Equal(t, []int64{1002, 1003, 1004, 1005}, guildClient.request.GetTargetUserIds())
	require.Equal(t, []int64{1001, 1002, 1003, 1004}, presenceClient.request.GetUserIds())
	require.Equal(t, []int64{1001, 1002, 1003, 1004}, publicPresenceUserIDs(resp.GetPresences()))
	require.Equal(t, apiv1.PresenceStatus_PRESENCE_STATUS_OFFLINE, resp.GetPresences()[1].GetStatus())
	require.Equal(t, int64(13), resp.GetPresences()[2].GetVersion())
	require.Equal(t, apiv1.PresenceStatus_PRESENCE_STATUS_DND, resp.GetPresences()[3].GetStatus())
}

func TestResolveUsersPresenceRejectsInvalidAndOversizedInput(t *testing.T) {
	client, closeServer := newPresenceHTTPClient(
		t, new(presenceUserClient), new(presenceGuildClient), new(presenceInternalClient),
	)
	defer closeServer()

	req := new(apiv1.ResolveUsersPresenceRequest)
	req.SetUserIds([]int64{0})
	_, err := client.ResolveUsersPresence(t.Context(), req)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))

	userIDs := make([]int64, maxPresenceResolveUsers+1)
	for i := range userIDs {
		userIDs[i] = int64(i + 1)
	}
	req.SetUserIds(userIDs)
	_, err = client.ResolveUsersPresence(t.Context(), req)
	require.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
}

func TestResolveUsersPresenceRejectsIncompleteInternalResponse(t *testing.T) {
	userClient := &presenceUserClient{relationships: []*userv1.Relationship{
		relationship(1001, 1002, userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND),
	}}
	client, closeServer := newPresenceHTTPClient(
		t, userClient, new(presenceGuildClient),
		&presenceInternalClient{presences: []*presencev1.UserPresence{
			internalPresence(1001, presencev1.PresenceStatus_PRESENCE_STATUS_ONLINE, 11),
		}},
	)
	defer closeServer()

	req := new(apiv1.ResolveUsersPresenceRequest)
	req.SetUserIds([]int64{1001, 1002})
	_, err := client.ResolveUsersPresence(t.Context(), req)
	require.Equal(t, connect.CodeInternal, connect.CodeOf(err))
}

func newPresenceHTTPClient(
	t *testing.T,
	userClient userv1.UserServiceClient,
	guildClient guildv1.GuildServiceClient,
	presenceClient presencev1.PresenceServiceClient,
) (apiv1connect.PresenceServiceClient, func()) {
	t.Helper()
	svcCtx := &svc.ServiceContext{
		AuthenticatorClient: &fakeAuthenticatorClient{verifyResponse: verifyAccessTokenResponse(1001)},
		UserClient:          userClient, GuildClient: guildClient, PresenceClient: presenceClient,
	}
	path, handler := apiv1connect.NewPresenceServiceHandler(NewPresence(svcCtx))
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	httpServer := httptest.NewServer(mux)
	httpClient := &http.Client{Transport: bearerRoundTripper{
		base: http.DefaultTransport, accessToken: "access-token",
	}}
	return apiv1connect.NewPresenceServiceClient(httpClient, httpServer.URL), httpServer.Close
}

func relationship(userID, targetID int64, relationshipType userv1.RelationshipType) *userv1.Relationship {
	value := new(userv1.Relationship)
	value.SetUserId(userID)
	value.SetTargetId(targetID)
	value.SetType(relationshipType)
	return value
}

func internalPresence(
	userID int64,
	status presencev1.PresenceStatus,
	version int64,
) *presencev1.UserPresence {
	value := new(presencev1.UserPresence)
	value.SetUserId(userID)
	value.SetStatus(status)
	value.SetLastSeenAt(userID + 10)
	value.SetVersion(version)
	session := new(presencev1.UserSession)
	session.SetSessionId("private-session")
	value.SetSessions([]*presencev1.UserSession{session})
	return value
}

func publicPresenceUserIDs(values []*apiv1.Presence) []int64 {
	result := make([]int64, 0, len(values))
	for _, value := range values {
		result = append(result, value.GetUserId())
	}
	return result
}

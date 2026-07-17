package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/pkg/rpcerror"
)

func (f *fakeUserClient) GetUserProfileByUsername(_ context.Context, req *userv1.GetUserProfileByUsernameRequest, _ ...grpc.CallOption) (*userv1.GetUserProfileByUsernameResponse, error) {
	f.getUserProfileByUsernameRequest = req
	return f.getUserProfileByUsernameResponse, f.getUserProfileByUsernameError
}

func (f *fakeUserClient) SendFriendRequest(_ context.Context, req *userv1.SendFriendRequestRequest, _ ...grpc.CallOption) (*userv1.SendFriendRequestResponse, error) {
	f.sendFriendRequestRequest = req
	return f.sendFriendRequestResponse, f.sendFriendRequestError
}

func (f *fakeUserClient) AcceptFriendRequest(_ context.Context, req *userv1.AcceptFriendRequestRequest, _ ...grpc.CallOption) (*userv1.AcceptFriendRequestResponse, error) {
	f.acceptFriendRequestRequest = req
	return f.acceptFriendRequestResponse, f.acceptFriendRequestError
}

func (f *fakeUserClient) DeclineFriendRequest(_ context.Context, req *userv1.DeclineFriendRequestRequest, _ ...grpc.CallOption) (*userv1.DeclineFriendRequestResponse, error) {
	f.declineFriendRequestRequest = req
	return f.declineFriendRequestResponse, f.declineFriendRequestError
}

func (f *fakeUserClient) RemoveFriend(_ context.Context, req *userv1.RemoveFriendRequest, _ ...grpc.CallOption) (*userv1.RemoveFriendResponse, error) {
	f.removeFriendRequest = req
	return f.removeFriendResponse, f.removeFriendError
}

func (f *fakeUserClient) BlockUser(_ context.Context, req *userv1.BlockUserRequest, _ ...grpc.CallOption) (*userv1.BlockUserResponse, error) {
	f.blockUserRequest = req
	return f.blockUserResponse, f.blockUserError
}

func (f *fakeUserClient) UnblockUser(_ context.Context, req *userv1.UnblockUserRequest, _ ...grpc.CallOption) (*userv1.UnblockUserResponse, error) {
	f.unblockUserRequest = req
	return f.unblockUserResponse, f.unblockUserError
}

func (f *fakeUserClient) ListRelationships(_ context.Context, req *userv1.ListRelationshipsRequest, _ ...grpc.CallOption) (*userv1.ListRelationshipsResponse, error) {
	f.listRelationshipsRequest = req
	return f.listRelationshipsResponse, f.listRelationshipsError
}

func TestLookupUserForwardsUsername(t *testing.T) {
	t.Run("authenticated", func(t *testing.T) {
		profile := internalUserProfile()
		profile.SetUsername("testuser")
		svcResp := new(userv1.GetUserProfileByUsernameResponse)
		svcResp.SetProfile(profile)

		authenticatorClient := &fakeAuthenticatorClient{
			verifyResponse: verifyAccessTokenResponse(1001),
		}
		userClient := &fakeUserClient{
			getUserProfileByUsernameResponse: svcResp,
		}
		client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
		defer closeServer()

		resp, err := client.LookupUser(context.Background(), &apiv1.LookupUserRequest{
			Username: new("testuser"),
		})
		require.NoError(t, err)
		require.Equal(t, "testuser", userClient.getUserProfileByUsernameRequest.GetUsername())
		require.Equal(t, "testuser", resp.GetProfile().GetUsername())
		require.Equal(t, int64(1001), resp.GetProfile().GetUserId())
		require.Equal(t, "display name", resp.GetProfile().GetName())
	})

	t.Run("unauthenticated", func(t *testing.T) {
		userClient := &fakeUserClient{}
		client, closeServer := newUserHTTPClient(t, &fakeAuthenticatorClient{}, userClient, "")
		defer closeServer()

		_, err := client.LookupUser(context.Background(), &apiv1.LookupUserRequest{
			Username: new("testuser"),
		})
		require.Equal(t, connect.CodeUnauthenticated, connect.CodeOf(err))
		require.Equal(t, apierror.CodeInvalidAccessToken, publicErrorInfo(t, err).GetCode())
		require.Nil(t, userClient.getUserProfileByUsernameRequest)
	})
}

func TestSendFriendRequestUsesAuthenticatedUser(t *testing.T) {
	svcResp := new(userv1.SendFriendRequestResponse)
	svcResp.SetRelationship(internalRelationship())

	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	userClient := &fakeUserClient{
		sendFriendRequestResponse: svcResp,
	}
	client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
	defer closeServer()

	resp, err := client.SendFriendRequest(context.Background(), &apiv1.SendFriendRequestRequest{
		TargetId: new(int64(1002)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), userClient.sendFriendRequestRequest.GetUserId())
	require.Equal(t, int64(1002), userClient.sendFriendRequestRequest.GetTargetId())
	require.Equal(t, int64(1002), resp.GetRelationship().GetTargetId())
	require.Equal(t, apiv1.RelationshipType_RELATIONSHIP_TYPE_OUTGOING, resp.GetRelationship().GetType())
	require.Equal(t, int64(2001), resp.GetRelationship().GetCreatedAt())
}

func TestRelationshipMutationsUseAuthenticatedUser(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}

	t.Run("accept friend request", func(t *testing.T) {
		svcResp := new(userv1.AcceptFriendRequestResponse)
		svcResp.SetRelationship(internalRelationship())
		userClient := &fakeUserClient{
			acceptFriendRequestResponse: svcResp,
		}
		client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
		defer closeServer()

		resp, err := client.AcceptFriendRequest(context.Background(), &apiv1.AcceptFriendRequestRequest{
			TargetId: new(int64(1002)),
		})
		require.NoError(t, err)
		require.Equal(t, int64(1001), userClient.acceptFriendRequestRequest.GetUserId())
		require.Equal(t, int64(1002), userClient.acceptFriendRequestRequest.GetTargetId())
		require.NotNil(t, resp.GetRelationship())
	})

	t.Run("decline friend request", func(t *testing.T) {
		svcResp := new(userv1.DeclineFriendRequestResponse)
		svcResp.SetOk(true)
		userClient := &fakeUserClient{
			declineFriendRequestResponse: svcResp,
		}
		client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
		defer closeServer()

		resp, err := client.DeclineFriendRequest(context.Background(), &apiv1.DeclineFriendRequestRequest{
			TargetId: new(int64(1002)),
		})
		require.NoError(t, err)
		require.Equal(t, int64(1001), userClient.declineFriendRequestRequest.GetUserId())
		require.Equal(t, int64(1002), userClient.declineFriendRequestRequest.GetTargetId())
		require.True(t, resp.GetOk())
	})

	t.Run("remove friend", func(t *testing.T) {
		svcResp := new(userv1.RemoveFriendResponse)
		svcResp.SetOk(true)
		userClient := &fakeUserClient{
			removeFriendResponse: svcResp,
		}
		client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
		defer closeServer()

		resp, err := client.RemoveFriend(context.Background(), &apiv1.RemoveFriendRequest{
			TargetId: new(int64(1002)),
		})
		require.NoError(t, err)
		require.Equal(t, int64(1001), userClient.removeFriendRequest.GetUserId())
		require.Equal(t, int64(1002), userClient.removeFriendRequest.GetTargetId())
		require.True(t, resp.GetOk())
	})

	t.Run("block user", func(t *testing.T) {
		svcResp := new(userv1.BlockUserResponse)
		svcResp.SetRelationship(internalBlockRelationship())
		userClient := &fakeUserClient{
			blockUserResponse: svcResp,
		}
		client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
		defer closeServer()

		resp, err := client.BlockUser(context.Background(), &apiv1.BlockUserRequest{
			TargetId: new(int64(1002)),
		})
		require.NoError(t, err)
		require.Equal(t, int64(1001), userClient.blockUserRequest.GetUserId())
		require.Equal(t, int64(1002), userClient.blockUserRequest.GetTargetId())
		require.NotNil(t, resp.GetRelationship())
	})

	t.Run("unblock user", func(t *testing.T) {
		svcResp := new(userv1.UnblockUserResponse)
		svcResp.SetOk(true)
		userClient := &fakeUserClient{
			unblockUserResponse: svcResp,
		}
		client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
		defer closeServer()

		resp, err := client.UnblockUser(context.Background(), &apiv1.UnblockUserRequest{
			TargetId: new(int64(1002)),
		})
		require.NoError(t, err)
		require.Equal(t, int64(1001), userClient.unblockUserRequest.GetUserId())
		require.Equal(t, int64(1002), userClient.unblockUserRequest.GetTargetId())
		require.True(t, resp.GetOk())
	})
}

func TestListRelationshipsMapsRequestAndResponse(t *testing.T) {
	svcResp := new(userv1.ListRelationshipsResponse)
	svcResp.SetRelationships([]*userv1.Relationship{internalRelationship()})
	svcResp.SetBeforeTargetId(1002)

	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}
	userClient := &fakeUserClient{
		listRelationshipsResponse: svcResp,
	}
	client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
	defer closeServer()

	resp, err := client.ListRelationships(context.Background(), &apiv1.ListRelationshipsRequest{
		Type:           new(apiv1.RelationshipType(apiv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND)),
		BeforeTargetId: new(int64(9999)),
		Limit:          new(int32(50)),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), userClient.listRelationshipsRequest.GetUserId())
	require.Equal(t, userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND, userClient.listRelationshipsRequest.GetType())
	require.Equal(t, int64(9999), userClient.listRelationshipsRequest.GetBeforeTargetId())
	require.Equal(t, int32(50), userClient.listRelationshipsRequest.GetLimit())
	require.Len(t, resp.GetRelationships(), 1)
	require.Equal(t, int64(1002), resp.GetRelationships()[0].GetTargetId())
	require.Equal(t, int64(1002), resp.GetBeforeTargetId())
}

func TestRelationshipErrorMappings(t *testing.T) {
	authenticatorClient := &fakeAuthenticatorClient{
		verifyResponse: verifyAccessTokenResponse(1001),
	}

	tests := map[string]struct {
		userClient  *fakeUserClient
		call        func(apiv1connect.UserServiceClient) error
		connectCode connect.Code
		publicCode  string
	}{
		"relationship not found": {
			userClient: &fakeUserClient{
				sendFriendRequestError: rpcerror.New(codes.NotFound, rpcerror.UserDomain, rpcerror.UserRelationshipNotFound, "relationship not found"),
			},
			call: func(c apiv1connect.UserServiceClient) error {
				_, err := c.SendFriendRequest(context.Background(), &apiv1.SendFriendRequestRequest{TargetId: new(int64(1002))})
				return err
			},
			connectCode: connect.CodeNotFound,
			publicCode:  apierror.CodeNotFound,
		},
		"relationship already exists": {
			userClient: &fakeUserClient{
				sendFriendRequestError: rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserRelationshipAlreadyExists, "relationship already exists"),
			},
			call: func(c apiv1connect.UserServiceClient) error {
				_, err := c.SendFriendRequest(context.Background(), &apiv1.SendFriendRequestRequest{TargetId: new(int64(1002))})
				return err
			},
			connectCode: connect.CodeAlreadyExists,
			publicCode:  apierror.CodeAlreadyExists,
		},
		"relationship blocked": {
			userClient: &fakeUserClient{
				sendFriendRequestError: rpcerror.New(codes.PermissionDenied, rpcerror.UserDomain, rpcerror.UserRelationshipBlocked, "blocked"),
			},
			call: func(c apiv1connect.UserServiceClient) error {
				_, err := c.SendFriendRequest(context.Background(), &apiv1.SendFriendRequestRequest{TargetId: new(int64(1002))})
				return err
			},
			connectCode: connect.CodePermissionDenied,
			publicCode:  apierror.CodePermissionDenied,
		},
		"username taken": {
			userClient: &fakeUserClient{
				getUserProfileByUsernameError: rpcerror.New(codes.AlreadyExists, rpcerror.UserDomain, rpcerror.UserUsernameTaken, "username taken"),
			},
			call: func(c apiv1connect.UserServiceClient) error {
				_, err := c.LookupUser(context.Background(), &apiv1.LookupUserRequest{Username: new("taken")})
				return err
			},
			connectCode: connect.CodeAlreadyExists,
			publicCode:  apierror.CodeAlreadyExists,
		},
		"unmapped gRPC status": {
			userClient: &fakeUserClient{
				sendFriendRequestError: status.Error(codes.InvalidArgument, "bad input"),
			},
			call: func(c apiv1connect.UserServiceClient) error {
				_, err := c.SendFriendRequest(context.Background(), &apiv1.SendFriendRequestRequest{TargetId: new(int64(1002))})
				return err
			},
			connectCode: connect.CodeInvalidArgument,
			publicCode:  apierror.CodeInvalidArgument,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			client, closeServer := newUserHTTPClient(t, authenticatorClient, tt.userClient, "access-token")
			defer closeServer()
			err := tt.call(client)
			require.Equal(t, tt.connectCode, connect.CodeOf(err))
			require.Equal(t, tt.publicCode, publicErrorInfo(t, err).GetCode())
		})
	}
}

func internalRelationship() *userv1.Relationship {
	rel := new(userv1.Relationship)
	rel.SetUserId(1001)
	rel.SetTargetId(1002)
	rel.SetType(userv1.RelationshipType_RELATIONSHIP_TYPE_OUTGOING)
	rel.SetCreatedAt(2001)
	rel.SetUpdatedAt(3001)
	return rel
}

func internalBlockRelationship() *userv1.Relationship {
	rel := new(userv1.Relationship)
	rel.SetUserId(1001)
	rel.SetTargetId(1002)
	rel.SetType(userv1.RelationshipType_RELATIONSHIP_TYPE_BLOCKED)
	rel.SetCreatedAt(2001)
	rel.SetUpdatedAt(3001)
	return rel
}

func (f *fakeUserClient) UpdateUsername(_ context.Context, req *userv1.UpdateUsernameRequest, _ ...grpc.CallOption) (*userv1.UpdateUsernameResponse, error) {
	f.updateUsernameRequest = req
	if f.updateUsernameError != nil {
		return nil, f.updateUsernameError
	}
	return f.updateUsernameResponse, nil
}

func TestUpdateUsernameUsesAuthenticatedUser(t *testing.T) {
	profile := new(userv1.UserProfile)
	profile.SetUserId(1001)
	profile.SetUsername("fresh_name")
	svcResp := new(userv1.UpdateUsernameResponse)
	svcResp.SetProfile(profile)

	authenticatorClient := &fakeAuthenticatorClient{verifyResponse: verifyAccessTokenResponse(1001)}
	userClient := &fakeUserClient{updateUsernameResponse: svcResp}
	client, closeServer := newUserHTTPClient(t, authenticatorClient, userClient, "access-token")
	defer closeServer()

	resp, err := client.UpdateUsername(context.Background(), &apiv1.UpdateUsernameRequest{
		Username: new("Fresh_Name"),
	})
	require.NoError(t, err)
	require.Equal(t, int64(1001), userClient.updateUsernameRequest.GetUserId())
	require.Equal(t, "Fresh_Name", userClient.updateUsernameRequest.GetUsername())
	require.Equal(t, "fresh_name", resp.GetProfile().GetUsername())
}

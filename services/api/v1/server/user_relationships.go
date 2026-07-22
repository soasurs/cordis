package server

import (
	"context"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
)

func (s *userServer) LookupUser(ctx context.Context, req *apiv1.LookupUserRequest) (*apiv1.LookupUserResponse, error) {
	if _, err := authenticate(ctx, s.svcCtx.AuthenticatorClient); err != nil {
		return nil, err
	}

	svcReq := new(userv1.GetUserProfileByUsernameRequest)
	svcReq.SetUsername(req.GetUsername())
	svcResp, err := s.svcCtx.UserClient.GetUserProfileByUsername(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.LookupUserResponse)
	resp.SetProfile(userProfileToAPI(svcResp.GetProfile()))
	return resp, nil
}

func (s *userServer) SendFriendRequest(ctx context.Context, req *apiv1.SendFriendRequestRequest) (*apiv1.SendFriendRequestResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	if err := checkRelationshipWrite(ctx, auth.GetUserId()); err != nil {
		return nil, err
	}
	if err := checkUserPolicy(ctx, apiratelimit.PolicySendFriendRequestMinute, auth.GetUserId()); err != nil {
		return nil, err
	}
	if err := checkUserPolicy(ctx, apiratelimit.PolicySendFriendRequestDay, auth.GetUserId()); err != nil {
		return nil, err
	}

	svcReq := new(userv1.SendFriendRequestRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetTargetId(req.GetTargetId())
	svcResp, err := s.svcCtx.UserClient.SendFriendRequest(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.SendFriendRequestResponse)
	resp.SetRelationship(relationshipToAPI(svcResp.GetRelationship()))
	return resp, nil
}

func (s *userServer) AcceptFriendRequest(ctx context.Context, req *apiv1.AcceptFriendRequestRequest) (*apiv1.AcceptFriendRequestResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	if err := checkRelationshipWrite(ctx, auth.GetUserId()); err != nil {
		return nil, err
	}

	svcReq := new(userv1.AcceptFriendRequestRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetTargetId(req.GetTargetId())
	svcResp, err := s.svcCtx.UserClient.AcceptFriendRequest(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.AcceptFriendRequestResponse)
	resp.SetRelationship(relationshipToAPI(svcResp.GetRelationship()))
	return resp, nil
}

func (s *userServer) DeclineFriendRequest(ctx context.Context, req *apiv1.DeclineFriendRequestRequest) (*apiv1.DeclineFriendRequestResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	if err := checkRelationshipWrite(ctx, auth.GetUserId()); err != nil {
		return nil, err
	}

	svcReq := new(userv1.DeclineFriendRequestRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetTargetId(req.GetTargetId())
	svcResp, err := s.svcCtx.UserClient.DeclineFriendRequest(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.DeclineFriendRequestResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
}

func (s *userServer) RemoveFriend(ctx context.Context, req *apiv1.RemoveFriendRequest) (*apiv1.RemoveFriendResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	if err := checkRelationshipWrite(ctx, auth.GetUserId()); err != nil {
		return nil, err
	}

	svcReq := new(userv1.RemoveFriendRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetTargetId(req.GetTargetId())
	svcResp, err := s.svcCtx.UserClient.RemoveFriend(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.RemoveFriendResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
}

func (s *userServer) BlockUser(ctx context.Context, req *apiv1.BlockUserRequest) (*apiv1.BlockUserResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	if err := checkRelationshipWrite(ctx, auth.GetUserId()); err != nil {
		return nil, err
	}
	if err := checkUserPairPolicy(ctx, apiratelimit.PolicyBlockUnblockDebounce, auth.GetUserId(), req.GetTargetId()); err != nil {
		return nil, err
	}

	svcReq := new(userv1.BlockUserRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetTargetId(req.GetTargetId())
	svcResp, err := s.svcCtx.UserClient.BlockUser(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.BlockUserResponse)
	resp.SetRelationship(relationshipToAPI(svcResp.GetRelationship()))
	return resp, nil
}

func (s *userServer) UnblockUser(ctx context.Context, req *apiv1.UnblockUserRequest) (*apiv1.UnblockUserResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	if err := checkRelationshipWrite(ctx, auth.GetUserId()); err != nil {
		return nil, err
	}
	if err := checkUserPairPolicy(ctx, apiratelimit.PolicyBlockUnblockDebounce, auth.GetUserId(), req.GetTargetId()); err != nil {
		return nil, err
	}

	svcReq := new(userv1.UnblockUserRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetTargetId(req.GetTargetId())
	svcResp, err := s.svcCtx.UserClient.UnblockUser(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.UnblockUserResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
}

func (s *userServer) ListRelationships(ctx context.Context, req *apiv1.ListRelationshipsRequest) (*apiv1.ListRelationshipsResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}

	svcReq := new(userv1.ListRelationshipsRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetType(userv1.RelationshipType(req.GetType()))
	svcReq.SetBeforeTargetId(req.GetBeforeTargetId())
	svcReq.SetLimit(req.GetLimit())
	svcResp, err := s.svcCtx.UserClient.ListRelationships(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.ListRelationshipsResponse)
	resp.SetRelationships(relationshipsToAPI(svcResp.GetRelationships()))
	resp.SetBeforeTargetId(svcResp.GetBeforeTargetId())
	return resp, nil
}

func relationshipToAPI(relationship *userv1.Relationship) *apiv1.Relationship {
	if relationship == nil {
		return nil
	}
	resp := new(apiv1.Relationship)
	resp.SetTargetId(relationship.GetTargetId())
	resp.SetType(apiv1.RelationshipType(relationship.GetType()))
	resp.SetCreatedAt(relationship.GetCreatedAt())
	resp.SetUpdatedAt(relationship.GetUpdatedAt())
	return resp
}

func relationshipsToAPI(relationships []*userv1.Relationship) []*apiv1.Relationship {
	values := make([]*apiv1.Relationship, 0, len(relationships))
	for _, relationship := range relationships {
		values = append(values, relationshipToAPI(relationship))
	}
	return values
}

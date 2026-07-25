package server

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
)

func (s *guildServer) CreateGuildInvite(ctx context.Context, req *apiv1.CreateGuildInviteRequest) (*apiv1.CreateGuildInviteResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	if err := checkGuildResourceCreate(ctx, auth.GetUserId(), req.GetGuildId()); err != nil {
		return nil, err
	}
	svcReq := new(guildv1.CreateGuildInviteRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetMaxUses(req.GetMaxUses())
	svcReq.SetExpiresInMs(req.GetExpiresInMs())
	svcResp, err := s.svcCtx.GuildClient.CreateGuildInvite(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	invites, err := s.guildInvitesToAPIWithProfiles(ctx, []*guildv1.GuildInvite{svcResp.GetInvite()})
	if err != nil {
		return nil, err
	}
	resp := new(apiv1.CreateGuildInviteResponse)
	resp.SetInvite(invites[0])
	return resp, nil
}

func (s *guildServer) GetGuildInvite(ctx context.Context, req *apiv1.GetGuildInviteRequest) (*apiv1.GetGuildInviteResponse, error) {
	if _, err := authenticate(ctx, s.svcCtx.AuthenticatorClient); err != nil {
		return nil, err
	}
	svcReq := new(guildv1.GetGuildInviteRequest)
	svcReq.SetCode(req.GetCode())
	svcResp, err := s.svcCtx.GuildClient.GetGuildInvite(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.GetGuildInviteResponse)
	resp.SetPreview(guildInvitePreviewToAPI(svcResp.GetPreview()))
	return resp, nil
}

func (s *guildServer) ListGuildInvites(ctx context.Context, req *apiv1.ListGuildInvitesRequest) (*apiv1.ListGuildInvitesResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.ListGuildInvitesRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetBeforeId(req.GetBeforeId())
	svcReq.SetLimit(req.GetLimit())
	svcResp, err := s.svcCtx.GuildClient.ListGuildInvites(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	invites, err := s.guildInvitesToAPIWithProfiles(ctx, svcResp.GetInvites())
	if err != nil {
		return nil, err
	}
	resp := new(apiv1.ListGuildInvitesResponse)
	resp.SetInvites(invites)
	resp.SetBeforeId(svcResp.GetBeforeId())
	return resp, nil
}

func (s *guildServer) DeleteGuildInvite(ctx context.Context, req *apiv1.DeleteGuildInviteRequest) (*apiv1.DeleteGuildInviteResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.DeleteGuildInviteRequest)
	svcReq.SetCode(req.GetCode())
	svcReq.SetActorUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.DeleteGuildInvite(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.DeleteGuildInviteResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
}

func (s *guildServer) JoinGuildByInvite(ctx context.Context, req *apiv1.JoinGuildByInviteRequest) (*apiv1.JoinGuildByInviteResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	if err := checkUserPolicy(ctx, apiratelimit.PolicyJoinGuildInviteUser, auth.GetUserId()); err != nil {
		return nil, err
	}
	if err := apiratelimit.CheckIP(ctx, apiratelimit.PolicyJoinGuildInviteIP); err != nil {
		return nil, err
	}
	svcReq := new(guildv1.JoinGuildByInviteRequest)
	svcReq.SetCode(req.GetCode())
	svcReq.SetUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.JoinGuildByInvite(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	members, err := s.guildMembersToAPIWithProfiles(ctx, []*guildv1.GuildMember{svcResp.GetMember()})
	if err != nil {
		return nil, err
	}
	resp := new(apiv1.JoinGuildByInviteResponse)
	resp.SetGuild(guildToAPI(svcResp.GetGuild()))
	resp.SetMember(members[0])
	return resp, nil
}

func (s *guildServer) guildInvitesToAPIWithProfiles(
	ctx context.Context,
	invites []*guildv1.GuildInvite,
) ([]*apiv1.GuildInvite, error) {
	userIDs := make([]int64, 0, len(invites))
	for _, invite := range invites {
		if invite == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("guild service returned an invalid invite"))
		}
		userIDs = append(userIDs, invite.GetCreatorUserId())
	}
	profiles, err := getUserProfiles(ctx, s.svcCtx.UserClient, userIDs)
	if err != nil {
		return nil, err
	}
	values := guildInvitesToAPI(invites)
	for i, invite := range invites {
		values[i].SetCreator(userProfileToAPI(profiles[invite.GetCreatorUserId()]))
	}
	return values, nil
}

func guildInviteToAPI(invite *guildv1.GuildInvite) *apiv1.GuildInvite {
	if invite == nil {
		return nil
	}
	resp := new(apiv1.GuildInvite)
	resp.SetId(invite.GetId())
	resp.SetCode(invite.GetCode())
	resp.SetGuildId(invite.GetGuildId())
	resp.SetCreatorUserId(invite.GetCreatorUserId())
	resp.SetMaxUses(invite.GetMaxUses())
	resp.SetUses(invite.GetUses())
	resp.SetExpiresAt(invite.GetExpiresAt())
	resp.SetCreatedAt(invite.GetCreatedAt())
	return resp
}

func guildInvitesToAPI(invites []*guildv1.GuildInvite) []*apiv1.GuildInvite {
	values := make([]*apiv1.GuildInvite, 0, len(invites))
	for _, invite := range invites {
		values = append(values, guildInviteToAPI(invite))
	}
	return values
}

func guildInvitePreviewToAPI(preview *guildv1.GuildInvitePreview) *apiv1.GuildInvitePreview {
	if preview == nil {
		return nil
	}
	resp := new(apiv1.GuildInvitePreview)
	resp.SetCode(preview.GetCode())
	resp.SetGuildId(preview.GetGuildId())
	resp.SetGuildName(preview.GetGuildName())
	resp.SetGuildIconAssetId(preview.GetGuildIconAssetId())
	resp.SetMemberCount(preview.GetMemberCount())
	resp.SetExpiresAt(preview.GetExpiresAt())
	return resp
}

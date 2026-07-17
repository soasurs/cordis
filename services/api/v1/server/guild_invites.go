package server

import (
	"context"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/apierror"
)

func (s *guildServer) CreateGuildInvite(ctx context.Context, req *apiv1.CreateGuildInviteRequest) (*apiv1.CreateGuildInviteResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
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
	return &apiv1.CreateGuildInviteResponse{Invite: guildInviteToAPI(svcResp.GetInvite())}, nil
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
	return &apiv1.GetGuildInviteResponse{Preview: guildInvitePreviewToAPI(svcResp.GetPreview())}, nil
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
	return &apiv1.ListGuildInvitesResponse{
		Invites:  guildInvitesToAPI(svcResp.GetInvites()),
		BeforeId: new(svcResp.GetBeforeId()),
	}, nil
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
	return &apiv1.DeleteGuildInviteResponse{Ok: new(svcResp.GetOk())}, nil
}

func (s *guildServer) JoinGuildByInvite(ctx context.Context, req *apiv1.JoinGuildByInviteRequest) (*apiv1.JoinGuildByInviteResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.JoinGuildByInviteRequest)
	svcReq.SetCode(req.GetCode())
	svcReq.SetUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.JoinGuildByInvite(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.JoinGuildByInviteResponse{
		Guild:  guildToAPI(svcResp.GetGuild()),
		Member: guildMemberToAPI(svcResp.GetMember()),
	}, nil
}

func guildInviteToAPI(invite *guildv1.GuildInvite) *apiv1.GuildInvite {
	if invite == nil {
		return nil
	}
	return &apiv1.GuildInvite{
		Id:            new(invite.GetId()),
		Code:          new(invite.GetCode()),
		GuildId:       new(invite.GetGuildId()),
		CreatorUserId: new(invite.GetCreatorUserId()),
		MaxUses:       new(invite.GetMaxUses()),
		Uses:          new(invite.GetUses()),
		ExpiresAt:     new(invite.GetExpiresAt()),
		CreatedAt:     new(invite.GetCreatedAt()),
	}
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
	return &apiv1.GuildInvitePreview{
		Code:         new(preview.GetCode()),
		GuildId:      new(preview.GetGuildId()),
		GuildName:    new(preview.GetGuildName()),
		GuildIconUri: new(preview.GetGuildIconUri()),
		MemberCount:  new(preview.GetMemberCount()),
		ExpiresAt:    new(preview.GetExpiresAt()),
	}
}

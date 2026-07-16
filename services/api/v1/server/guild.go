package server

import (
	"context"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

type guildServer struct {
	svcCtx *svc.ServiceContext
}

func NewGuild(svcCtx *svc.ServiceContext) apiv1connect.GuildServiceHandler {
	return &guildServer{svcCtx: svcCtx}
}

func (s *guildServer) CreateGuild(ctx context.Context, req *apiv1.CreateGuildRequest) (*apiv1.CreateGuildResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.CreateGuildRequest)
	svcReq.SetOwnerId(auth.GetUserId())
	svcReq.SetName(req.GetName())
	svcReq.SetIconUri(req.GetIconUri())
	svcResp, err := s.svcCtx.GuildClient.CreateGuild(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.CreateGuildResponse{Guild: guildToAPI(svcResp.GetGuild())}, nil
}

func (s *guildServer) GetGuild(ctx context.Context, req *apiv1.GetGuildRequest) (*apiv1.GetGuildResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.GetGuildRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.GetGuild(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.GetGuildResponse{Guild: guildToAPI(svcResp.GetGuild())}, nil
}

func (s *guildServer) ListGuilds(ctx context.Context, req *apiv1.ListGuildsRequest) (*apiv1.ListGuildsResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.ListUserGuildsRequest)
	svcReq.SetUserId(auth.GetUserId())
	svcReq.SetBefore(req.GetBefore())
	svcReq.SetLimit(req.GetLimit())
	svcResp, err := s.svcCtx.GuildClient.ListUserGuilds(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.ListGuildsResponse{
		Guilds:       guildsToAPI(svcResp.GetGuilds()),
		BeforeCursor: new(svcResp.GetBeforeCursor()),
	}, nil
}

func (s *guildServer) UpdateGuild(ctx context.Context, req *apiv1.UpdateGuildRequest) (*apiv1.UpdateGuildResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.UpdateGuildRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	if req.Name != nil {
		svcReq.SetName(req.GetName())
	}
	if req.IconUri != nil {
		svcReq.SetIconUri(req.GetIconUri())
	}
	svcResp, err := s.svcCtx.GuildClient.UpdateGuild(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.UpdateGuildResponse{Guild: guildToAPI(svcResp.GetGuild())}, nil
}

func (s *guildServer) DeleteGuild(ctx context.Context, req *apiv1.DeleteGuildRequest) (*apiv1.DeleteGuildResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.DeleteGuildRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.DeleteGuild(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.DeleteGuildResponse{Ok: new(svcResp.GetOk())}, nil
}

func (s *guildServer) AddGuildMember(ctx context.Context, req *apiv1.AddGuildMemberRequest) (*apiv1.AddGuildMemberResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.AddGuildMemberRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetUserId(req.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.AddGuildMember(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.AddGuildMemberResponse{Member: guildMemberToAPI(svcResp.GetMember())}, nil
}

func (s *guildServer) GetGuildMember(ctx context.Context, req *apiv1.GetGuildMemberRequest) (*apiv1.GetGuildMemberResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.GetGuildMemberRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetUserId(req.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.GetGuildMember(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.GetGuildMemberResponse{Member: guildMemberToAPI(svcResp.GetMember())}, nil
}

func (s *guildServer) ListGuildMembers(ctx context.Context, req *apiv1.ListGuildMembersRequest) (*apiv1.ListGuildMembersResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.ListGuildMembersRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetBeforeUserId(req.GetBeforeUserId())
	svcReq.SetLimit(req.GetLimit())
	svcResp, err := s.svcCtx.GuildClient.ListGuildMembers(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.ListGuildMembersResponse{
		Members:      guildMembersToAPI(svcResp.GetMembers()),
		BeforeUserId: new(svcResp.GetBeforeUserId()),
	}, nil
}

func (s *guildServer) UpdateCurrentGuildMember(ctx context.Context, req *apiv1.UpdateCurrentGuildMemberRequest) (*apiv1.UpdateCurrentGuildMemberResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.UpdateGuildMemberRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetNickname(req.GetNickname())
	svcResp, err := s.svcCtx.GuildClient.UpdateGuildMember(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.UpdateCurrentGuildMemberResponse{Member: guildMemberToAPI(svcResp.GetMember())}, nil
}

func (s *guildServer) KickGuildMember(ctx context.Context, req *apiv1.KickGuildMemberRequest) (*apiv1.KickGuildMemberResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.KickGuildMemberRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetUserId(req.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.KickGuildMember(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.KickGuildMemberResponse{Ok: new(svcResp.GetOk())}, nil
}

func (s *guildServer) LeaveGuild(ctx context.Context, req *apiv1.LeaveGuildRequest) (*apiv1.LeaveGuildResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.LeaveGuildRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.LeaveGuild(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.LeaveGuildResponse{Ok: new(svcResp.GetOk())}, nil
}

func (s *guildServer) TransferGuildOwnership(ctx context.Context, req *apiv1.TransferGuildOwnershipRequest) (*apiv1.TransferGuildOwnershipResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.TransferGuildOwnershipRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetNewOwnerId(req.GetNewOwnerId())
	svcResp, err := s.svcCtx.GuildClient.TransferGuildOwnership(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.TransferGuildOwnershipResponse{Guild: guildToAPI(svcResp.GetGuild())}, nil
}

func guildToAPI(guild *guildv1.Guild) *apiv1.Guild {
	if guild == nil {
		return nil
	}
	return &apiv1.Guild{
		Id:        new(guild.GetId()),
		OwnerId:   new(guild.GetOwnerId()),
		Name:      new(guild.GetName()),
		IconUri:   new(guild.GetIconUri()),
		Revision:  new(guild.GetRevision()),
		CreatedAt: new(guild.GetCreatedAt()),
		UpdatedAt: new(guild.GetUpdatedAt()),
	}
}

func guildsToAPI(guilds []*guildv1.Guild) []*apiv1.Guild {
	values := make([]*apiv1.Guild, 0, len(guilds))
	for _, guild := range guilds {
		values = append(values, guildToAPI(guild))
	}
	return values
}

func guildMemberToAPI(member *guildv1.GuildMember) *apiv1.GuildMember {
	if member == nil {
		return nil
	}
	return &apiv1.GuildMember{
		GuildId:   new(member.GetGuildId()),
		UserId:    new(member.GetUserId()),
		Nickname:  new(member.GetNickname()),
		Revision:  new(member.GetRevision()),
		JoinedAt:  new(member.GetJoinedAt()),
		UpdatedAt: new(member.GetUpdatedAt()),
	}
}

func guildMembersToAPI(members []*guildv1.GuildMember) []*apiv1.GuildMember {
	values := make([]*apiv1.GuildMember, 0, len(members))
	for _, member := range members {
		values = append(values, guildMemberToAPI(member))
	}
	return values
}

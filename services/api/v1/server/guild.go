package server

import (
	"context"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
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
	if err := checkUserPolicy(ctx, apiratelimit.PolicyCreateGuildUser, auth.GetUserId()); err != nil {
		return nil, err
	}
	svcReq := new(guildv1.CreateGuildRequest)
	svcReq.SetOwnerId(auth.GetUserId())
	svcReq.SetName(req.GetName())
	svcResp, err := s.svcCtx.GuildClient.CreateGuild(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.CreateGuildResponse)
	resp.SetGuild(guildToAPI(svcResp.GetGuild()))
	return resp, nil
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
	resp := new(apiv1.GetGuildResponse)
	resp.SetGuild(guildToAPI(svcResp.GetGuild()))
	return resp, nil
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
	resp := new(apiv1.ListGuildsResponse)
	resp.SetGuilds(guildsToAPI(svcResp.GetGuilds()))
	resp.SetBeforeCursor(svcResp.GetBeforeCursor())
	return resp, nil
}

func (s *guildServer) UpdateGuild(ctx context.Context, req *apiv1.UpdateGuildRequest) (*apiv1.UpdateGuildResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.UpdateGuildRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	if req.HasName() {
		svcReq.SetName(req.GetName())
	}
	svcResp, err := s.svcCtx.GuildClient.UpdateGuild(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.UpdateGuildResponse)
	resp.SetGuild(guildToAPI(svcResp.GetGuild()))
	return resp, nil
}

func (s *guildServer) CreateGuildIconUpload(
	ctx context.Context,
	req *apiv1.CreateGuildIconUploadRequest,
) (*apiv1.CreateGuildIconUploadResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.CreateGuildIconUploadRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetExpectedSize(req.GetExpectedSize())
	svcReq.SetContentType(req.GetContentType())
	svcResp, err := s.svcCtx.GuildClient.CreateGuildIconUpload(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.CreateGuildIconUploadResponse)
	resp.SetUploadId(svcResp.GetUploadId())
	resp.SetPresignedUrl(svcResp.GetPresignedUrl())
	resp.SetExpiresAt(svcResp.GetExpiresAt())
	return resp, nil
}

func (s *guildServer) CompleteGuildIconUpload(
	ctx context.Context,
	req *apiv1.CompleteGuildIconUploadRequest,
) (*apiv1.CompleteGuildIconUploadResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.CompleteGuildIconUploadRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetUploadId(req.GetUploadId())
	svcResp, err := s.svcCtx.GuildClient.CompleteGuildIconUpload(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.CompleteGuildIconUploadResponse)
	resp.SetGuild(guildToAPI(svcResp.GetGuild()))
	return resp, nil
}

func (s *guildServer) AbortGuildIconUpload(
	ctx context.Context,
	req *apiv1.AbortGuildIconUploadRequest,
) (*apiv1.AbortGuildIconUploadResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.AbortGuildIconUploadRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetUploadId(req.GetUploadId())
	if _, err := s.svcCtx.GuildClient.AbortGuildIconUpload(ctx, svcReq); err != nil {
		return nil, apierror.FromRPC(err)
	}
	return new(apiv1.AbortGuildIconUploadResponse), nil
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
	resp := new(apiv1.DeleteGuildResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
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
	resp := new(apiv1.AddGuildMemberResponse)
	resp.SetMember(guildMemberToAPI(svcResp.GetMember()))
	return resp, nil
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
	resp := new(apiv1.GetGuildMemberResponse)
	resp.SetMember(guildMemberToAPI(svcResp.GetMember()))
	return resp, nil
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
	resp := new(apiv1.ListGuildMembersResponse)
	resp.SetMembers(guildMembersToAPI(svcResp.GetMembers()))
	resp.SetBeforeUserId(svcResp.GetBeforeUserId())
	return resp, nil
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
	resp := new(apiv1.UpdateCurrentGuildMemberResponse)
	resp.SetMember(guildMemberToAPI(svcResp.GetMember()))
	return resp, nil
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
	resp := new(apiv1.KickGuildMemberResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
}

func (s *guildServer) BanGuildMember(ctx context.Context, req *apiv1.BanGuildMemberRequest) (*apiv1.BanGuildMemberResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.BanGuildMemberRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetUserId(req.GetUserId())
	svcReq.SetReason(req.GetReason())
	svcResp, err := s.svcCtx.GuildClient.BanGuildMember(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.BanGuildMemberResponse)
	resp.SetBan(guildBanToAPI(svcResp.GetBan()))
	return resp, nil
}

func (s *guildServer) UnbanGuildMember(ctx context.Context, req *apiv1.UnbanGuildMemberRequest) (*apiv1.UnbanGuildMemberResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.UnbanGuildMemberRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetUserId(req.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.UnbanGuildMember(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.UnbanGuildMemberResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
}

func (s *guildServer) ListGuildBans(ctx context.Context, req *apiv1.ListGuildBansRequest) (*apiv1.ListGuildBansResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.ListGuildBansRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetBeforeUserId(req.GetBeforeUserId())
	svcReq.SetLimit(req.GetLimit())
	svcResp, err := s.svcCtx.GuildClient.ListGuildBans(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.ListGuildBansResponse)
	resp.SetBans(guildBansToAPI(svcResp.GetBans()))
	resp.SetBeforeUserId(svcResp.GetBeforeUserId())
	return resp, nil
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
	resp := new(apiv1.LeaveGuildResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
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
	resp := new(apiv1.TransferGuildOwnershipResponse)
	resp.SetGuild(guildToAPI(svcResp.GetGuild()))
	return resp, nil
}

func guildToAPI(guild *guildv1.Guild) *apiv1.Guild {
	if guild == nil {
		return nil
	}
	resp := new(apiv1.Guild)
	resp.SetId(guild.GetId())
	resp.SetOwnerId(guild.GetOwnerId())
	resp.SetName(guild.GetName())
	resp.SetIconAssetId(guild.GetIconAssetId())
	resp.SetRevision(guild.GetRevision())
	resp.SetCreatedAt(guild.GetCreatedAt())
	resp.SetUpdatedAt(guild.GetUpdatedAt())
	return resp
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
	resp := new(apiv1.GuildMember)
	resp.SetGuildId(member.GetGuildId())
	resp.SetUserId(member.GetUserId())
	resp.SetNickname(member.GetNickname())
	resp.SetRevision(member.GetRevision())
	resp.SetJoinedAt(member.GetJoinedAt())
	resp.SetUpdatedAt(member.GetUpdatedAt())
	return resp
}

func guildMembersToAPI(members []*guildv1.GuildMember) []*apiv1.GuildMember {
	values := make([]*apiv1.GuildMember, 0, len(members))
	for _, member := range members {
		values = append(values, guildMemberToAPI(member))
	}
	return values
}

func guildBanToAPI(ban *guildv1.GuildBan) *apiv1.GuildBan {
	if ban == nil {
		return nil
	}
	resp := new(apiv1.GuildBan)
	resp.SetGuildId(ban.GetGuildId())
	resp.SetUserId(ban.GetUserId())
	resp.SetActorUserId(ban.GetActorUserId())
	resp.SetReason(ban.GetReason())
	resp.SetCreatedAt(ban.GetCreatedAt())
	return resp
}

func guildBansToAPI(bans []*guildv1.GuildBan) []*apiv1.GuildBan {
	values := make([]*apiv1.GuildBan, 0, len(bans))
	for _, ban := range bans {
		values = append(values, guildBanToAPI(ban))
	}
	return values
}

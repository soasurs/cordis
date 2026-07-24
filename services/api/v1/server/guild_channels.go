package server

import (
	"context"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/apierror"
)

func (s *guildServer) CreateGuildChannel(ctx context.Context, req *apiv1.CreateGuildChannelRequest) (*apiv1.CreateGuildChannelResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	if err := checkGuildResourceCreate(ctx, auth.GetUserId(), req.GetGuildId()); err != nil {
		return nil, err
	}
	svcReq := new(guildv1.CreateGuildChannelRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetName(req.GetName())
	svcReq.SetType(guildv1.GuildChannelType(req.GetType()))
	svcReq.SetTopic(req.GetTopic())
	svcReq.SetParentId(req.GetParentId())
	svcResp, err := s.svcCtx.GuildClient.CreateGuildChannel(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.CreateGuildChannelResponse)
	resp.SetChannel(guildChannelToAPI(svcResp.GetChannel()))
	return resp, nil
}

func (s *guildServer) GetGuildChannel(ctx context.Context, req *apiv1.GetGuildChannelRequest) (*apiv1.GetGuildChannelResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.GetGuildChannelRequest)
	svcReq.SetChannelId(req.GetChannelId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.GetGuildChannel(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.GetGuildChannelResponse)
	resp.SetChannel(guildChannelToAPI(svcResp.GetChannel()))
	return resp, nil
}

func (s *guildServer) ListGuildChannels(ctx context.Context, req *apiv1.ListGuildChannelsRequest) (*apiv1.ListGuildChannelsResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.ListGuildChannelsRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.ListGuildChannels(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.ListGuildChannelsResponse)
	resp.SetChannels(guildChannelsToAPI(svcResp.GetChannels()))
	return resp, nil
}

func (s *guildServer) UpdateGuildChannel(ctx context.Context, req *apiv1.UpdateGuildChannelRequest) (*apiv1.UpdateGuildChannelResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.UpdateGuildChannelRequest)
	svcReq.SetChannelId(req.GetChannelId())
	svcReq.SetActorUserId(auth.GetUserId())
	if req.HasName() {
		svcReq.SetName(req.GetName())
	}
	if req.HasTopic() {
		svcReq.SetTopic(req.GetTopic())
	}
	if req.HasParentId() {
		svcReq.SetParentId(req.GetParentId())
	}
	svcResp, err := s.svcCtx.GuildClient.UpdateGuildChannel(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.UpdateGuildChannelResponse)
	resp.SetChannel(guildChannelToAPI(svcResp.GetChannel()))
	return resp, nil
}

func (s *guildServer) DeleteGuildChannel(ctx context.Context, req *apiv1.DeleteGuildChannelRequest) (*apiv1.DeleteGuildChannelResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.DeleteGuildChannelRequest)
	svcReq.SetChannelId(req.GetChannelId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.DeleteGuildChannel(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.DeleteGuildChannelResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
}

func (s *guildServer) ReorderGuildChannels(ctx context.Context, req *apiv1.ReorderGuildChannelsRequest) (*apiv1.ReorderGuildChannelsResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	positions := make([]*guildv1.GuildChannelPosition, 0, len(req.GetPositions()))
	for _, item := range req.GetPositions() {
		position := new(guildv1.GuildChannelPosition)
		position.SetChannelId(item.GetChannelId())
		position.SetPosition(item.GetPosition())
		if item.HasParentId() {
			position.SetParentId(item.GetParentId())
		}
		positions = append(positions, position)
	}
	svcReq := new(guildv1.ReorderGuildChannelsRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetPositions(positions)
	svcResp, err := s.svcCtx.GuildClient.ReorderGuildChannels(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.ReorderGuildChannelsResponse)
	resp.SetChannels(guildChannelsToAPI(svcResp.GetChannels()))
	return resp, nil
}

func (s *guildServer) UpsertGuildChannelPermissionOverwrite(ctx context.Context, req *apiv1.UpsertGuildChannelPermissionOverwriteRequest) (*apiv1.UpsertGuildChannelPermissionOverwriteResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.UpsertGuildChannelPermissionOverwriteRequest)
	svcReq.SetChannelId(req.GetChannelId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetTargetType(guildv1.GuildPermissionOverwriteType(req.GetTargetType()))
	svcReq.SetTargetId(req.GetTargetId())
	svcReq.SetAllow(req.GetAllow())
	svcReq.SetDeny(req.GetDeny())
	svcResp, err := s.svcCtx.GuildClient.UpsertGuildChannelPermissionOverwrite(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.UpsertGuildChannelPermissionOverwriteResponse)
	resp.SetOverwrite(guildChannelOverwriteToAPI(svcResp.GetOverwrite()))
	return resp, nil
}

func (s *guildServer) DeleteGuildChannelPermissionOverwrite(ctx context.Context, req *apiv1.DeleteGuildChannelPermissionOverwriteRequest) (*apiv1.DeleteGuildChannelPermissionOverwriteResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.DeleteGuildChannelPermissionOverwriteRequest)
	svcReq.SetChannelId(req.GetChannelId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetTargetType(guildv1.GuildPermissionOverwriteType(req.GetTargetType()))
	svcReq.SetTargetId(req.GetTargetId())
	svcResp, err := s.svcCtx.GuildClient.DeleteGuildChannelPermissionOverwrite(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.DeleteGuildChannelPermissionOverwriteResponse)
	resp.SetOk(svcResp.GetOk())
	return resp, nil
}

func (s *guildServer) ListGuildChannelPermissionOverwrites(ctx context.Context, req *apiv1.ListGuildChannelPermissionOverwritesRequest) (*apiv1.ListGuildChannelPermissionOverwritesResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.ListGuildChannelPermissionOverwritesRequest)
	svcReq.SetChannelId(req.GetChannelId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.ListGuildChannelPermissionOverwrites(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.ListGuildChannelPermissionOverwritesResponse)
	resp.SetOverwrites(guildChannelOverwritesToAPI(svcResp.GetOverwrites()))
	return resp, nil
}

func guildChannelToAPI(channel *guildv1.GuildChannel) *apiv1.GuildChannel {
	if channel == nil {
		return nil
	}
	resp := new(apiv1.GuildChannel)
	resp.SetId(channel.GetId())
	resp.SetGuildId(channel.GetGuildId())
	resp.SetName(channel.GetName())
	resp.SetType(apiv1.GuildChannelType(channel.GetType()))
	resp.SetPosition(channel.GetPosition())
	resp.SetTopic(channel.GetTopic())
	resp.SetRevision(channel.GetRevision())
	resp.SetCreatedAt(channel.GetCreatedAt())
	resp.SetUpdatedAt(channel.GetUpdatedAt())
	resp.SetParentId(channel.GetParentId())
	return resp
}

func guildChannelsToAPI(channels []*guildv1.GuildChannel) []*apiv1.GuildChannel {
	values := make([]*apiv1.GuildChannel, 0, len(channels))
	for _, channel := range channels {
		values = append(values, guildChannelToAPI(channel))
	}
	return values
}

func guildChannelOverwriteToAPI(overwrite *guildv1.GuildChannelPermissionOverwrite) *apiv1.GuildChannelPermissionOverwrite {
	if overwrite == nil {
		return nil
	}
	resp := new(apiv1.GuildChannelPermissionOverwrite)
	resp.SetChannelId(overwrite.GetChannelId())
	resp.SetGuildId(overwrite.GetGuildId())
	resp.SetTargetType(apiv1.GuildPermissionOverwriteType(overwrite.GetTargetType()))
	resp.SetTargetId(overwrite.GetTargetId())
	resp.SetAllow(overwrite.GetAllow())
	resp.SetDeny(overwrite.GetDeny())
	resp.SetRevision(overwrite.GetRevision())
	resp.SetCreatedAt(overwrite.GetCreatedAt())
	resp.SetUpdatedAt(overwrite.GetUpdatedAt())
	return resp
}

func guildChannelOverwritesToAPI(overwrites []*guildv1.GuildChannelPermissionOverwrite) []*apiv1.GuildChannelPermissionOverwrite {
	values := make([]*apiv1.GuildChannelPermissionOverwrite, 0, len(overwrites))
	for _, overwrite := range overwrites {
		values = append(values, guildChannelOverwriteToAPI(overwrite))
	}
	return values
}

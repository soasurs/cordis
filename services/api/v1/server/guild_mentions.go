package server

import (
	"context"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/apierror"
)

func (s *guildServer) SearchGuildMentionUsers(ctx context.Context, req *apiv1.SearchGuildMentionUsersRequest) (*apiv1.SearchGuildMentionUsersResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.SearchGuildMentionUsersRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetChannelId(req.GetChannelId())
	svcReq.SetQuery(req.GetQuery())
	svcReq.SetLimit(req.GetLimit())
	svcResp, err := s.svcCtx.GuildClient.SearchGuildMentionUsers(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.SearchGuildMentionUsersResponse)
	resp.SetUsers(guildMentionUsersToAPI(svcResp.GetUsers()))
	return resp, nil
}

func (s *guildServer) SearchGuildMentionRoles(ctx context.Context, req *apiv1.SearchGuildMentionRolesRequest) (*apiv1.SearchGuildMentionRolesResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.SearchGuildMentionRolesRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetQuery(req.GetQuery())
	svcReq.SetLimit(req.GetLimit())
	svcResp, err := s.svcCtx.GuildClient.SearchGuildMentionRoles(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	resp := new(apiv1.SearchGuildMentionRolesResponse)
	resp.SetRoles(guildRolesToAPI(svcResp.GetRoles()))
	return resp, nil
}

func guildMentionUserToAPI(user *guildv1.GuildMentionUser) *apiv1.GuildMentionUser {
	if user == nil {
		return nil
	}
	resp := new(apiv1.GuildMentionUser)
	resp.SetUserId(user.GetUserId())
	resp.SetUsername(user.GetUsername())
	resp.SetName(user.GetName())
	resp.SetAvatarAssetId(user.GetAvatarAssetId())
	resp.SetNickname(user.GetNickname())
	return resp
}

func guildMentionUsersToAPI(users []*guildv1.GuildMentionUser) []*apiv1.GuildMentionUser {
	values := make([]*apiv1.GuildMentionUser, 0, len(users))
	for _, user := range users {
		values = append(values, guildMentionUserToAPI(user))
	}
	return values
}

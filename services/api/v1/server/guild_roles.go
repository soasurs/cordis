package server

import (
	"context"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/pkg/apierror"
)

func (s *guildServer) CreateGuildRole(ctx context.Context, req *apiv1.CreateGuildRoleRequest) (*apiv1.CreateGuildRoleResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.CreateGuildRoleRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetName(req.GetName())
	svcReq.SetPermissions(req.GetPermissions())
	svcResp, err := s.svcCtx.GuildClient.CreateGuildRole(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.CreateGuildRoleResponse{Role: guildRoleToAPI(svcResp.GetRole())}, nil
}

func (s *guildServer) GetGuildRole(ctx context.Context, req *apiv1.GetGuildRoleRequest) (*apiv1.GetGuildRoleResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.GetGuildRoleRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetRoleId(req.GetRoleId())
	svcResp, err := s.svcCtx.GuildClient.GetGuildRole(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.GetGuildRoleResponse{Role: guildRoleToAPI(svcResp.GetRole())}, nil
}

func (s *guildServer) ListGuildRoles(ctx context.Context, req *apiv1.ListGuildRolesRequest) (*apiv1.ListGuildRolesResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.ListGuildRolesRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.ListGuildRoles(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.ListGuildRolesResponse{Roles: guildRolesToAPI(svcResp.GetRoles())}, nil
}

func (s *guildServer) UpdateGuildRole(ctx context.Context, req *apiv1.UpdateGuildRoleRequest) (*apiv1.UpdateGuildRoleResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.UpdateGuildRoleRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetRoleId(req.GetRoleId())
	if req.Name != nil {
		svcReq.SetName(req.GetName())
	}
	if req.Permissions != nil {
		svcReq.SetPermissions(req.GetPermissions())
	}
	svcResp, err := s.svcCtx.GuildClient.UpdateGuildRole(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.UpdateGuildRoleResponse{Role: guildRoleToAPI(svcResp.GetRole())}, nil
}

func (s *guildServer) DeleteGuildRole(ctx context.Context, req *apiv1.DeleteGuildRoleRequest) (*apiv1.DeleteGuildRoleResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.DeleteGuildRoleRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetRoleId(req.GetRoleId())
	svcResp, err := s.svcCtx.GuildClient.DeleteGuildRole(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.DeleteGuildRoleResponse{Ok: new(svcResp.GetOk())}, nil
}

func (s *guildServer) ReorderGuildRoles(ctx context.Context, req *apiv1.ReorderGuildRolesRequest) (*apiv1.ReorderGuildRolesResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	positions := make([]*guildv1.GuildRolePosition, 0, len(req.GetPositions()))
	for _, position := range req.GetPositions() {
		value := new(guildv1.GuildRolePosition)
		value.SetRoleId(position.GetRoleId())
		value.SetPosition(position.GetPosition())
		positions = append(positions, value)
	}
	svcReq := new(guildv1.ReorderGuildRolesRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetPositions(positions)
	svcResp, err := s.svcCtx.GuildClient.ReorderGuildRoles(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.ReorderGuildRolesResponse{Roles: guildRolesToAPI(svcResp.GetRoles())}, nil
}

func (s *guildServer) AddGuildMemberRole(ctx context.Context, req *apiv1.AddGuildMemberRoleRequest) (*apiv1.AddGuildMemberRoleResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.AddGuildMemberRoleRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetUserId(req.GetUserId())
	svcReq.SetRoleId(req.GetRoleId())
	svcResp, err := s.svcCtx.GuildClient.AddGuildMemberRole(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.AddGuildMemberRoleResponse{Ok: new(svcResp.GetOk())}, nil
}

func (s *guildServer) RemoveGuildMemberRole(ctx context.Context, req *apiv1.RemoveGuildMemberRoleRequest) (*apiv1.RemoveGuildMemberRoleResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.RemoveGuildMemberRoleRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetUserId(req.GetUserId())
	svcReq.SetRoleId(req.GetRoleId())
	svcResp, err := s.svcCtx.GuildClient.RemoveGuildMemberRole(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.RemoveGuildMemberRoleResponse{Ok: new(svcResp.GetOk())}, nil
}

func (s *guildServer) ListGuildMemberRoles(ctx context.Context, req *apiv1.ListGuildMemberRolesRequest) (*apiv1.ListGuildMemberRolesResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.ListGuildMemberRolesRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetUserId(req.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.ListGuildMemberRoles(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.ListGuildMemberRolesResponse{Roles: guildRolesToAPI(svcResp.GetRoles())}, nil
}

func (s *guildServer) GetGuildMemberPermissions(ctx context.Context, req *apiv1.GetGuildMemberPermissionsRequest) (*apiv1.GetGuildMemberPermissionsResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	svcReq := new(guildv1.GetGuildMemberPermissionsRequest)
	svcReq.SetGuildId(req.GetGuildId())
	svcReq.SetActorUserId(auth.GetUserId())
	svcReq.SetUserId(req.GetUserId())
	svcResp, err := s.svcCtx.GuildClient.GetGuildMemberPermissions(ctx, svcReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	return &apiv1.GetGuildMemberPermissionsResponse{Permissions: new(svcResp.GetPermissions())}, nil
}

func guildRoleToAPI(role *guildv1.GuildRole) *apiv1.GuildRole {
	if role == nil {
		return nil
	}
	return &apiv1.GuildRole{
		Id:          new(role.GetId()),
		GuildId:     new(role.GetGuildId()),
		Name:        new(role.GetName()),
		Permissions: new(role.GetPermissions()),
		Position:    new(role.GetPosition()),
		IsDefault:   new(role.GetIsDefault()),
		Revision:    new(role.GetRevision()),
		CreatedAt:   new(role.GetCreatedAt()),
		UpdatedAt:   new(role.GetUpdatedAt()),
	}
}

func guildRolesToAPI(roles []*guildv1.GuildRole) []*apiv1.GuildRole {
	values := make([]*apiv1.GuildRole, 0, len(roles))
	for _, role := range roles {
		values = append(values, guildRoleToAPI(role))
	}
	return values
}

package server

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
)

type authorizingGuild struct {
	guildv1.GuildServiceClient
	allowed           bool
	accessRevision    int64
	visibleChannelIDs []int64
}

type failingVisibilityGuild struct {
	guildv1.GuildServiceClient
}

type concurrentVisibilityGuild struct {
	guildv1.GuildServiceClient
	started chan<- int64
	release <-chan struct{}
}

func (failingVisibilityGuild) GetUserReadyState(
	context.Context,
	*guildv1.GetUserReadyStateRequest,
	...grpc.CallOption,
) (*guildv1.GetUserReadyStateResponse, error) {
	return nil, status.Error(codes.Unavailable, "guild unavailable")
}

func (failingVisibilityGuild) GetUserGuildChannelVisibility(
	context.Context,
	*guildv1.GetUserGuildChannelVisibilityRequest,
	...grpc.CallOption,
) (*guildv1.GetUserGuildChannelVisibilityResponse, error) {
	return nil, status.Error(codes.Unavailable, "guild unavailable")
}

func (g *authorizingGuild) GetUserGuildChannelVisibility(
	_ context.Context,
	req *guildv1.GetUserGuildChannelVisibilityRequest,
	_ ...grpc.CallOption,
) (*guildv1.GetUserGuildChannelVisibilityResponse, error) {
	revision := g.accessRevision
	if revision <= 0 {
		revision = 8
	}
	channelIDs := append([]int64(nil), g.visibleChannelIDs...)
	if g.visibleChannelIDs == nil && g.allowed {
		channelIDs = []int64{7001}
	}
	resp := new(guildv1.GetUserGuildChannelVisibilityResponse)
	resp.SetVisibility(visibility(req.GetGuildId(), revision, channelIDs...))
	return resp, nil
}

func (g *concurrentVisibilityGuild) GetUserGuildChannelVisibility(
	ctx context.Context,
	req *guildv1.GetUserGuildChannelVisibilityRequest,
	_ ...grpc.CallOption,
) (*guildv1.GetUserGuildChannelVisibilityResponse, error) {
	select {
	case g.started <- req.GetUserId():
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-g.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	resp := new(guildv1.GetUserGuildChannelVisibilityResponse)
	resp.SetVisibility(visibility(req.GetGuildId(), 8, 7001))
	return resp, nil
}

package server

import (
	"context"
	"errors"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/sync/errgroup"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/dispatcher/v1/internal/discovery"
)

const profileDispatchConcurrency = 16

func (s *Server) dispatchUserProfile(
	ctx context.Context,
	userID int64,
	event eventEnvelope,
	idempotencyKey int64,
) error {
	var guildIDs, relationshipIDs, dmRecipientIDs []int64
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		var err error
		guildIDs, err = s.userGuildIDs(groupCtx, userID)
		return err
	})
	group.Go(func() error {
		var err error
		relationshipIDs, err = s.relationshipRecipientIDs(groupCtx, userID)
		return err
	})
	group.Go(func() error {
		var err error
		dmRecipientIDs, err = s.dmRecipientIDs(groupCtx, userID)
		return err
	})
	if err := group.Wait(); err != nil {
		return err
	}

	guildIDs = uniquePositiveIDs(guildIDs, 0)
	recipientIDs := uniquePositiveIDs(
		append(append(relationshipIDs, dmRecipientIDs...), userID),
		0,
	)
	logx.WithContext(ctx).Infow(
		"user profile fan-out",
		logx.Field("user_id", userID),
		logx.Field("guild_count", len(guildIDs)),
		logx.Field("recipient_count", len(recipientIDs)),
	)

	recipientGroup, recipientCtx := errgroup.WithContext(ctx)
	recipientGroup.SetLimit(profileDispatchConcurrency)
	for _, recipientID := range recipientIDs {
		recipientGroup.Go(func() error {
			return s.dispatchUser(recipientCtx, recipientID, event, idempotencyKey)
		})
	}
	if err := recipientGroup.Wait(); err != nil {
		return err
	}

	guildGroup, guildCtx := errgroup.WithContext(ctx)
	guildGroup.SetLimit(profileDispatchConcurrency)
	for _, guildID := range guildIDs {
		guildGroup.Go(func() error {
			nodes, err := s.resolver.Resolve(guildCtx, discovery.RouteGuild, guildID)
			if err != nil {
				return err
			}
			return s.forEachNode(guildCtx, nodes, func(ctx context.Context, client sessionv1.SessionServiceClient) error {
				req := new(sessionv1.DispatchGuildEventRequest)
				req.SetGuildId(guildID)
				req.SetEvent(protoEvent(event, idempotencyKey))
				_, err := client.DispatchGuildEvent(ctx, req)
				return err
			})
		})
	}
	return guildGroup.Wait()
}

func (s *Server) userGuildIDs(ctx context.Context, userID int64) ([]int64, error) {
	var guildIDs []int64
	var cursor string
	for {
		req := new(guildv1.ListUserGuildsRequest)
		req.SetUserId(userID)
		req.SetLimit(100)
		if cursor != "" {
			req.SetCursor(cursor)
		}
		resp, err := s.guildClient.ListUserGuilds(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, guild := range resp.GetGuilds() {
			if guild.GetId() > 0 {
				guildIDs = append(guildIDs, guild.GetId())
			}
		}
		if !resp.HasNextCursor() || len(resp.GetGuilds()) == 0 {
			return guildIDs, nil
		}
		cursor = resp.GetNextCursor()
	}
}

func (s *Server) relationshipRecipientIDs(ctx context.Context, userID int64) ([]int64, error) {
	var recipientIDs []int64
	var cursor string
	for {
		req := new(userv1.ListRelationshipsRequest)
		req.SetUserId(userID)
		req.SetLimit(200)
		if cursor != "" {
			req.SetCursor(cursor)
		}
		resp, err := s.userClient.ListRelationships(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, relationship := range resp.GetRelationships() {
			if relationship.GetUserId() != userID {
				return nil, errors.New("user service returned an unexpected relationship")
			}
			switch relationship.GetType() {
			case userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND,
				userv1.RelationshipType_RELATIONSHIP_TYPE_INCOMING,
				userv1.RelationshipType_RELATIONSHIP_TYPE_OUTGOING:
				recipientIDs = append(recipientIDs, relationship.GetTargetId())
			case userv1.RelationshipType_RELATIONSHIP_TYPE_BLOCKED:
			default:
				return nil, errors.New("user service returned an invalid relationship type")
			}
		}
		if !resp.HasNextCursor() || len(resp.GetRelationships()) == 0 {
			return recipientIDs, nil
		}
		cursor = resp.GetNextCursor()
	}
}

func (s *Server) dmRecipientIDs(ctx context.Context, userID int64) ([]int64, error) {
	var recipientIDs []int64
	var cursor string
	for {
		req := new(messagev1.ListDmChannelsRequest)
		req.SetUserId(userID)
		req.SetLimit(100)
		if cursor != "" {
			req.SetCursor(cursor)
		}
		resp, err := s.messageClient.ListDmChannels(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, channel := range resp.GetChannels() {
			switch userID {
			case channel.GetUserLo():
				recipientIDs = append(recipientIDs, channel.GetUserHi())
			case channel.GetUserHi():
				recipientIDs = append(recipientIDs, channel.GetUserLo())
			default:
				return nil, errors.New("message service returned an unexpected dm channel")
			}
		}
		if !resp.HasNextCursor() || len(resp.GetChannels()) == 0 {
			return recipientIDs, nil
		}
		cursor = resp.GetNextCursor()
	}
}

func uniquePositiveIDs(values []int64, excluded int64) []int64 {
	seen := make(map[int64]struct{}, len(values))
	result := make([]int64, 0, len(values))
	for _, value := range values {
		if value <= 0 || value == excluded {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

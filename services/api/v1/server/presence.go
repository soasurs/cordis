package server

import (
	"context"
	"errors"
	"slices"
	"strconv"

	"connectrpc.com/connect"

	apiv1 "github.com/soasurs/cordis/gen/api/v1"
	apiv1connect "github.com/soasurs/cordis/gen/api/v1/apiv1connect"
	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/apierror"
	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

const maxPresenceResolveUsers = 100

type presenceServer struct {
	svcCtx *svc.ServiceContext
}

func NewPresence(svcCtx *svc.ServiceContext) apiv1connect.PresenceServiceHandler {
	return &presenceServer{svcCtx: svcCtx}
}

func (s *presenceServer) ResolveUsersPresence(
	ctx context.Context,
	req *apiv1.ResolveUsersPresenceRequest,
) (*apiv1.ResolveUsersPresenceResponse, error) {
	auth, err := authenticate(ctx, s.svcCtx.AuthenticatorClient)
	if err != nil {
		return nil, err
	}
	if err := apiratelimit.CheckIP(ctx, apiratelimit.PolicyResolvePresenceIP); err != nil {
		return nil, err
	}
	if err := apiratelimit.CheckKey(
		ctx,
		apiratelimit.PolicyResolvePresenceUser,
		strconv.FormatInt(auth.GetUserId(), 10),
	); err != nil {
		return nil, err
	}

	userIDs := append([]int64(nil), req.GetUserIds()...)
	slices.Sort(userIDs)
	userIDs = slices.Compact(userIDs)
	if len(userIDs) > maxPresenceResolveUsers {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("too many users"))
	}
	for _, userID := range userIDs {
		if userID <= 0 {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user id is required"))
		}
	}
	if len(userIDs) == 0 {
		return new(apiv1.ResolveUsersPresenceResponse), nil
	}

	targetUserIDs := make([]int64, 0, len(userIDs))
	targets := make(map[int64]struct{}, len(userIDs))
	visible := map[int64]struct{}{auth.GetUserId(): {}}
	for _, userID := range userIDs {
		if userID != auth.GetUserId() {
			targetUserIDs = append(targetUserIDs, userID)
			targets[userID] = struct{}{}
		}
	}
	blocked := make(map[int64]struct{})
	if len(targetUserIDs) > 0 {
		relationshipReq := new(userv1.CheckRelationshipsRequest)
		relationshipReq.SetUserId(auth.GetUserId())
		relationshipReq.SetTargetIds(targetUserIDs)
		relationshipReq.SetIncludeReverse(true)
		relationshipResp, err := s.svcCtx.UserClient.CheckRelationships(ctx, relationshipReq)
		if err != nil {
			return nil, apierror.FromRPC(err)
		}
		for _, relationship := range relationshipResp.GetRelationships() {
			var targetID int64
			switch {
			case relationship.GetUserId() == auth.GetUserId():
				targetID = relationship.GetTargetId()
			case relationship.GetTargetId() == auth.GetUserId():
				targetID = relationship.GetUserId()
			default:
				return nil, connect.NewError(connect.CodeInternal, errors.New("user service returned an invalid response"))
			}
			if _, ok := targets[targetID]; !ok {
				return nil, connect.NewError(connect.CodeInternal, errors.New("user service returned an invalid response"))
			}
			switch relationship.GetType() {
			case userv1.RelationshipType_RELATIONSHIP_TYPE_BLOCKED:
				blocked[targetID] = struct{}{}
				delete(visible, targetID)
			case userv1.RelationshipType_RELATIONSHIP_TYPE_FRIEND:
				if _, denied := blocked[targetID]; !denied {
					visible[targetID] = struct{}{}
				}
			}
		}

		guildReq := new(guildv1.FilterUsersWithCommonGuildRequest)
		guildReq.SetUserId(auth.GetUserId())
		guildReq.SetTargetUserIds(targetUserIDs)
		guildResp, err := s.svcCtx.GuildClient.FilterUsersWithCommonGuild(ctx, guildReq)
		if err != nil {
			return nil, apierror.FromRPC(err)
		}
		for _, userID := range guildResp.GetUserIds() {
			if _, ok := targets[userID]; !ok {
				continue
			}
			if _, denied := blocked[userID]; !denied {
				visible[userID] = struct{}{}
			}
		}
	}

	visibleUserIDs := make([]int64, 0, len(visible))
	for _, userID := range userIDs {
		if _, ok := visible[userID]; ok {
			visibleUserIDs = append(visibleUserIDs, userID)
		}
	}
	if len(visibleUserIDs) == 0 {
		return new(apiv1.ResolveUsersPresenceResponse), nil
	}
	presenceReq := new(presencev1.ResolveUsersPresenceRequest)
	presenceReq.SetUserIds(visibleUserIDs)
	presenceResp, err := s.svcCtx.PresenceClient.ResolveUsersPresence(ctx, presenceReq)
	if err != nil {
		return nil, apierror.FromRPC(err)
	}
	allowed := make(map[int64]struct{}, len(visibleUserIDs))
	for _, userID := range visibleUserIDs {
		allowed[userID] = struct{}{}
	}
	seen := make(map[int64]struct{}, len(visibleUserIDs))
	presences := make([]*apiv1.Presence, 0, len(presenceResp.GetPresences()))
	for _, presence := range presenceResp.GetPresences() {
		if presence == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("presence service returned an invalid response"))
		}
		userID := presence.GetUserId()
		_, expected := allowed[userID]
		_, duplicate := seen[userID]
		if !expected || duplicate || presence.GetVersion() <= 0 {
			return nil, connect.NewError(connect.CodeInternal, errors.New("presence service returned an invalid response"))
		}
		seen[userID] = struct{}{}
		value := new(apiv1.Presence)
		value.SetUserId(userID)
		value.SetStatus(presenceStatusToAPI(presence.GetStatus()))
		value.SetLastSeenAt(presence.GetLastSeenAt())
		value.SetVersion(presence.GetVersion())
		presences = append(presences, value)
	}
	if len(seen) != len(visibleUserIDs) {
		return nil, connect.NewError(connect.CodeInternal, errors.New("presence service returned an invalid response"))
	}
	resp := new(apiv1.ResolveUsersPresenceResponse)
	resp.SetPresences(presences)
	return resp, nil
}

func presenceStatusToAPI(status presencev1.PresenceStatus) apiv1.PresenceStatus {
	switch status {
	case presencev1.PresenceStatus_PRESENCE_STATUS_ONLINE:
		return apiv1.PresenceStatus_PRESENCE_STATUS_ONLINE
	case presencev1.PresenceStatus_PRESENCE_STATUS_IDLE:
		return apiv1.PresenceStatus_PRESENCE_STATUS_IDLE
	case presencev1.PresenceStatus_PRESENCE_STATUS_DND:
		return apiv1.PresenceStatus_PRESENCE_STATUS_DND
	default:
		return apiv1.PresenceStatus_PRESENCE_STATUS_OFFLINE
	}
}

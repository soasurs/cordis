package server

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/realtime"
)

type eventRouting struct {
	ID             string `json:"id"`
	GuildID        string `json:"guild_id"`
	ChannelID      string `json:"channel_id"`
	UserID         string `json:"user_id"`
	OwnerID        string `json:"owner_id"`
	TargetID       string `json:"target_id"`
	TargetType     int32  `json:"target_type"`
	AccessRevision int64  `json:"access_revision"`
}

func (s *Server) DispatchGuildEvent(ctx context.Context, req *sessionv1.DispatchGuildEventRequest) (*sessionv1.DispatchGuildEventResponse, error) {
	if req.GetGuildId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "guild id is required")
	}
	eventType, payload, err := validateEvent(req.GetEvent())
	if err != nil {
		return nil, err
	}

	var routing eventRouting
	if err := json.Unmarshal(payload, &routing); err != nil {
		return nil, status.Error(codes.InvalidArgument, "event routing payload is invalid")
	}

	switch eventType {
	case realtime.EventGuildCreated:
		s.attachGuildUser(req.GetGuildId(), parseID(routing.OwnerID), routing.AccessRevision)
	case realtime.EventGuildMemberJoined:
		s.attachGuildUser(req.GetGuildId(), parseID(routing.UserID), routing.AccessRevision)
	case realtime.EventGuildMemberRemoved, realtime.EventGuildMemberBanned:
		// Revoke the snapshot before delivering the terminal membership event so
		// concurrent message dispatch fails closed during index cleanup.
		s.invalidateGuildVisibility(req.GetGuildId(), parseID(routing.UserID), routing.AccessRevision)
	case realtime.EventGuildUpdated, realtime.EventGuildRoleCreated:
		s.invalidateGuildVisibility(req.GetGuildId(), 0, routing.AccessRevision)
	case realtime.EventGuildRoleUpdated, realtime.EventGuildRoleDeleted:
		s.invalidateGuildVisibility(req.GetGuildId(), 0, routing.AccessRevision)
	case realtime.EventGuildMemberRolesUpdated:
		s.invalidateGuildVisibility(req.GetGuildId(), parseID(routing.UserID), routing.AccessRevision)
	case realtime.EventGuildChannelCreated, realtime.EventGuildChannelUpdated,
		realtime.EventGuildChannelOverwriteUpdated, realtime.EventGuildChannelOverwriteDeleted:
		s.invalidateChannelEventVisibility(req.GetGuildId(), eventType, routing)
		delivered := s.dispatchAuthorizedGuildChannel(ctx, req.GetGuildId(), eventType, payload, routing)
		return dispatchGuildResponse(delivered), nil
	case realtime.EventGuildChannelDeleted:
		s.invalidateGuildVisibility(req.GetGuildId(), 0, routing.AccessRevision)
		delivered := s.dispatchSessions(s.guildSessions(req.GetGuildId()), eventType, payload)
		return dispatchGuildResponse(delivered), nil
	}

	delivered := s.dispatchSessions(s.guildSessions(req.GetGuildId()), eventType, payload)
	switch eventType {
	case realtime.EventGuildMemberRemoved, realtime.EventGuildMemberBanned:
		s.detachGuildUser(req.GetGuildId(), parseID(routing.UserID))
	case realtime.EventGuildDeleted:
		s.detachGuild(req.GetGuildId())
	}
	return dispatchGuildResponse(delivered), nil
}

func (s *Server) invalidateChannelEventVisibility(guildID int64, eventType string, routing eventRouting) {
	if eventType == realtime.EventGuildChannelOverwriteUpdated || eventType == realtime.EventGuildChannelOverwriteDeleted {
		if routing.TargetType == int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER) {
			s.invalidateGuildVisibility(guildID, parseID(routing.TargetID), routing.AccessRevision)
			return
		}
	}
	s.invalidateGuildVisibility(guildID, 0, routing.AccessRevision)
}

func (s *Server) invalidateGuildVisibility(guildID, userID, accessRevision int64) {
	seen := make(map[int64]struct{})
	for _, session := range s.guildSessions(guildID) {
		if userID > 0 && session.userID != userID {
			continue
		}
		if _, exists := seen[session.userID]; exists {
			continue
		}
		seen[session.userID] = struct{}{}
		s.invalidateVisibilityGuild(session.userID, guildID, accessRevision)
	}
}

func (s *Server) DispatchGuildMessageEvent(ctx context.Context, req *sessionv1.DispatchGuildMessageEventRequest) (*sessionv1.DispatchGuildMessageEventResponse, error) {
	if req.GetGuildId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "guild id is required")
	}
	if req.GetChannelId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "channel id is required")
	}
	eventType, payload, err := validateEvent(req.GetEvent())
	if err != nil {
		return nil, err
	}
	switch eventType {
	case realtime.EventMessageCreated, realtime.EventMessageUpdated, realtime.EventMessageDeleted:
	default:
		return nil, status.Error(codes.InvalidArgument, "guild message event type is invalid")
	}
	resp := new(sessionv1.DispatchGuildMessageEventResponse)
	resp.SetDelivered(int32(s.dispatchVisibleGuildChannel(ctx, req.GetGuildId(), req.GetChannelId(), eventType, payload)))
	return resp, nil
}

func (s *Server) dispatchVisibleGuildChannel(
	ctx context.Context,
	guildID, channelID int64,
	eventType string,
	payload []byte,
) int {
	byUser := make(map[int64][]*logicalSession)
	for _, session := range s.guildSessions(guildID) {
		byUser[session.userID] = append(byUser[session.userID], session)
	}
	delivered := 0
	for userID, sessions := range byUser {
		snapshot, err := s.ensureVisibilitySnapshot(ctx, userID, guildID)
		if err != nil {
			logx.WithContext(ctx).Errorw("reload guild visibility for channel event",
				logx.Field("guild_id", guildID),
				logx.Field("channel_id", channelID),
				logx.Field("user_id", userID),
				logx.Field("error", err),
			)
			s.dispatchVisibilityReconcile(sessions, userID, guildID, channelID)
			continue
		}
		if snapshot.contains(channelID) {
			delivered += s.dispatchSessions(sessions, eventType, payload)
		}
	}
	return delivered
}

func (s *Server) dispatchVisibilityReconcile(sessions []*logicalSession, userID, guildID, channelID int64) {
	if !s.markVisibilityReconcile(userID, guildID) {
		return
	}
	payload, _ := json.Marshal(map[string]string{
		"guild_id":   strconv.FormatInt(guildID, 10),
		"channel_id": strconv.FormatInt(channelID, 10),
	})
	s.dispatchSessions(sessions, realtime.GatewayEventReconcile, payload)
}

func (s *Server) DispatchUserEvent(_ context.Context, req *sessionv1.DispatchUserEventRequest) (*sessionv1.DispatchUserEventResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "user id is required")
	}
	eventType, payload, err := validateEvent(req.GetEvent())
	if err != nil {
		return nil, err
	}
	resp := new(sessionv1.DispatchUserEventResponse)
	resp.SetDelivered(int32(s.dispatchSessions(s.userSessions(req.GetUserId()), eventType, payload)))
	return resp, nil
}

func (s *Server) dispatchAuthorizedGuildChannel(ctx context.Context, guildID int64, eventType string, payload []byte, routing eventRouting) int {
	channelID := parseID(routing.ChannelID)
	if channelID == 0 {
		channelID = parseID(routing.ID)
	}
	delivered := 0
	for _, session := range s.guildSessions(guildID) {
		allowed, err := s.authorizeChannel(ctx, session.userID, channelID)
		if err != nil {
			logx.WithContext(ctx).Errorw("authorize session guild channel event",
				logx.Field("guild_id", guildID),
				logx.Field("channel_id", channelID),
				logx.Field("user_id", session.userID),
				logx.Field("error", err),
			)
			continue
		}
		if !allowed {
			continue
		}
		if s.dispatchSession(session, eventType, payload) {
			delivered++
		}
	}
	return delivered
}

func (s *Server) dispatchSessions(sessions []*logicalSession, eventType string, payload []byte) int {
	delivered := 0
	for _, session := range sessions {
		if s.dispatchSession(session, eventType, payload) {
			delivered++
		}
	}
	return delivered
}

func (s *Server) dispatchSession(session *logicalSession, eventType string, payload []byte) bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.initializing {
		if session.pendingDispatchOverflow {
			return false
		}
		countLimit := s.svcCtx.Cfg.Node.PendingDispatchLimit()
		byteLimit := s.svcCtx.Cfg.Node.PendingDispatchByteLimit()
		eventBytes := int64(len(eventType)) + int64(len(payload))
		if len(session.pendingDispatches) >= countLimit ||
			eventBytes > byteLimit-session.pendingDispatchBytes {
			session.pendingDispatchOverflow = true
			session.pendingDispatches = nil
			session.pendingDispatchBytes = 0
			return false
		}
		session.pendingDispatches = append(session.pendingDispatches, pendingDispatch{
			eventType: eventType,
			payload:   append([]byte(nil), payload...),
		})
		session.pendingDispatchBytes += eventBytes
		return true
	}
	s.appendDispatchLocked(session, eventType, payload)
	return true
}

func (s *Server) attachGuildUser(guildID, userID, accessRevision int64) {
	if guildID <= 0 || userID <= 0 {
		return
	}
	sessions := s.userSessions(userID)
	for _, session := range sessions {
		session.mu.Lock()
		if _, exists := session.guilds[guildID]; !exists {
			session.guilds[guildID] = struct{}{}
		}
		session.mu.Unlock()
		s.mu.Lock()
		addIndex(s.guilds, guildID, session)
		s.mu.Unlock()
	}
	if len(sessions) > 0 {
		s.invalidateVisibilityGuild(userID, guildID, accessRevision)
	}
	s.refreshAllRoutes(context.Background())
}

func (s *Server) detachGuildUser(guildID, userID int64) {
	removed := false
	for _, session := range s.userSessions(userID) {
		session.mu.Lock()
		if _, exists := session.guilds[guildID]; exists {
			delete(session.guilds, guildID)
			removed = true
		}
		session.mu.Unlock()

		s.mu.Lock()
		removeIndex(s.guilds, guildID, session)
		s.mu.Unlock()
	}
	if removed {
		s.removeVisibilityGuild(userID, guildID)
	}
	s.refreshAllRoutes(context.Background())
}

func (s *Server) detachGuild(guildID int64) {
	userIDs := make(map[int64]struct{})
	for _, session := range s.guildSessions(guildID) {
		session.mu.Lock()
		delete(session.guilds, guildID)
		userIDs[session.userID] = struct{}{}
		session.mu.Unlock()
	}
	s.mu.Lock()
	delete(s.guilds, guildID)
	s.mu.Unlock()
	for userID := range userIDs {
		s.removeVisibilityGuild(userID, guildID)
	}
	s.refreshAllRoutes(context.Background())
}

func (s *Server) userSessions(userID int64) []*logicalSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sessionsFromSet(s.users[userID])
}

func (s *Server) guildSessions(guildID int64) []*logicalSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sessionsFromSet(s.guilds[guildID])
}

func sessionsFromSet(set map[*logicalSession]struct{}) []*logicalSession {
	sessions := make([]*logicalSession, 0, len(set))
	for session := range set {
		sessions = append(sessions, session)
	}
	return sessions
}

func validateEvent(event *sessionv1.EventEnvelope) (string, []byte, error) {
	if event == nil || strings.TrimSpace(event.GetType()) == "" {
		return "", nil, status.Error(codes.InvalidArgument, "event type is required")
	}
	payload := []byte(event.GetJsonPayload())
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	if !json.Valid(payload) {
		return "", nil, status.Error(codes.InvalidArgument, "event payload is invalid")
	}
	return event.GetType(), payload, nil
}

func dispatchGuildResponse(delivered int) *sessionv1.DispatchGuildEventResponse {
	resp := new(sessionv1.DispatchGuildEventResponse)
	resp.SetDelivered(int32(delivered))
	return resp
}

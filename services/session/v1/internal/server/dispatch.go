package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/pkg/realtime"
)

type eventRouting struct {
	ID        string `json:"id"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	OwnerID   string `json:"owner_id"`
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
		s.subscribeGuildUser(req.GetGuildId(), parseID(routing.OwnerID))
	case realtime.EventGuildMemberJoined:
		s.subscribeGuildUser(req.GetGuildId(), parseID(routing.UserID))
	case realtime.EventGuildRoleUpdated, realtime.EventGuildRoleDeleted:
		s.reauthorizeGuildSubscriptions(ctx, req.GetGuildId(), 0)
	case realtime.EventGuildMemberRolesUpdated:
		s.reauthorizeGuildSubscriptions(ctx, req.GetGuildId(), parseID(routing.UserID))
	case realtime.EventGuildChannelCreated, realtime.EventGuildChannelUpdated,
		realtime.EventGuildChannelOverwriteUpdated, realtime.EventGuildChannelOverwriteDeleted:
		delivered := s.dispatchAuthorizedGuildChannel(ctx, req.GetGuildId(), eventType, payload, routing)
		return dispatchGuildResponse(delivered), nil
	case realtime.EventGuildChannelDeleted:
		channelID := parseID(routing.ChannelID)
		if channelID == 0 {
			channelID = parseID(routing.ID)
		}
		delivered := s.dispatchSessions(s.guildSessions(req.GetGuildId()), eventType, payload)
		s.unsubscribeChannel(channelID)
		return dispatchGuildResponse(delivered), nil
	}

	delivered := s.dispatchSessions(s.guildSessions(req.GetGuildId()), eventType, payload)
	switch eventType {
	case realtime.EventGuildMemberRemoved, realtime.EventGuildMemberBanned:
		s.unsubscribeGuildUser(req.GetGuildId(), parseID(routing.UserID))
	case realtime.EventGuildDeleted:
		s.unsubscribeGuild(req.GetGuildId())
	}
	return dispatchGuildResponse(delivered), nil
}

func (s *Server) DispatchChannelEvent(_ context.Context, req *sessionv1.DispatchChannelEventRequest) (*sessionv1.DispatchChannelEventResponse, error) {
	if req.GetChannelId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "channel id is required")
	}
	eventType, payload, err := validateEvent(req.GetEvent())
	if err != nil {
		return nil, err
	}
	resp := new(sessionv1.DispatchChannelEventResponse)
	resp.SetDelivered(int32(s.dispatchSessions(s.channelSessions(req.GetChannelId()), eventType, payload)))
	return resp, nil
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
		allowed, _, err := s.authorizeChannel(ctx, session.userID, channelID)
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
			s.unsubscribeSessionChannel(session, channelID)
			continue
		}
		if s.dispatchSession(session, eventType, payload) {
			delivered++
		}
	}
	return delivered
}

func (s *Server) reauthorizeGuildSubscriptions(ctx context.Context, guildID, userID int64) {
	sessions := s.guildSessions(guildID)
	for _, session := range sessions {
		if userID > 0 && session.userID != userID {
			continue
		}
		session.mu.Lock()
		channelIDs := make([]int64, 0, len(session.channelGuilds))
		for channelID, channelGuildID := range session.channelGuilds {
			if channelGuildID == guildID {
				channelIDs = append(channelIDs, channelID)
			}
		}
		session.mu.Unlock()
		for _, channelID := range channelIDs {
			allowed, _, err := s.authorizeChannel(ctx, session.userID, channelID)
			if err != nil {
				logx.WithContext(ctx).Errorw("reauthorize session channel subscription",
					logx.Field("guild_id", guildID),
					logx.Field("channel_id", channelID),
					logx.Field("user_id", session.userID),
					logx.Field("error", err),
				)
				continue
			}
			if !allowed {
				s.unsubscribeSessionChannel(session, channelID)
			}
		}
	}
}

func (s *Server) unsubscribeSessionChannel(session *logicalSession, channelID int64) {
	session.mu.Lock()
	if _, subscribed := session.channels[channelID]; !subscribed {
		session.mu.Unlock()
		return
	}
	delete(session.channels, channelID)
	delete(session.channelGuilds, channelID)
	session.mu.Unlock()

	s.mu.Lock()
	removeIndex(s.channels, channelID, session)
	s.mu.Unlock()
	s.refreshAllRoutes(context.Background())
}

func (s *Server) unsubscribeChannel(channelID int64) {
	for _, session := range s.channelSessions(channelID) {
		session.mu.Lock()
		delete(session.channels, channelID)
		delete(session.channelGuilds, channelID)
		session.mu.Unlock()
	}
	s.mu.Lock()
	delete(s.channels, channelID)
	s.mu.Unlock()
	s.refreshAllRoutes(context.Background())
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
	s.appendDispatchLocked(session, eventType, payload)
	return true
}

func (s *Server) subscribeGuildUser(guildID, userID int64) {
	if guildID <= 0 || userID <= 0 {
		return
	}
	added := false
	for _, session := range s.userSessions(userID) {
		session.mu.Lock()
		if _, exists := session.guilds[guildID]; !exists {
			session.guilds[guildID] = struct{}{}
			added = true
		}
		session.mu.Unlock()
		s.mu.Lock()
		addIndex(s.guilds, guildID, session)
		s.mu.Unlock()
	}
	if added {
		s.invalidateVisibilityGuild(userID, guildID)
	}
	s.refreshAllRoutes(context.Background())
}

func (s *Server) unsubscribeGuildUser(guildID, userID int64) {
	removed := false
	for _, session := range s.userSessions(userID) {
		session.mu.Lock()
		if _, exists := session.guilds[guildID]; exists {
			delete(session.guilds, guildID)
			removed = true
		}
		var channelIDs []int64
		for channelID, channelGuildID := range session.channelGuilds {
			if channelGuildID == guildID {
				channelIDs = append(channelIDs, channelID)
				delete(session.channels, channelID)
				delete(session.channelGuilds, channelID)
			}
		}
		session.mu.Unlock()

		s.mu.Lock()
		removeIndex(s.guilds, guildID, session)
		for _, channelID := range channelIDs {
			removeIndex(s.channels, channelID, session)
		}
		s.mu.Unlock()
	}
	if removed {
		s.removeVisibilityGuild(userID, guildID)
	}
	s.refreshAllRoutes(context.Background())
}

func (s *Server) unsubscribeGuild(guildID int64) {
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

func (s *Server) channelSessions(channelID int64) []*logicalSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sessionsFromSet(s.channels[channelID])
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

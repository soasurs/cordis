package server

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"

	sessionv1 "github.com/soasurs/cordis/gen/session/v1"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
)

func (s *Server) detach(session *logicalSession, binding *binding, resumable bool) {
	session.mu.Lock()
	if session.binding != binding {
		session.mu.Unlock()
		return
	}
	session.binding = nil
	session.detachedAt = time.Now()
	session.mu.Unlock()
	binding.close()
	if !resumable {
		s.removeSession(context.Background(), session)
		return
	}
	_ = s.refreshOwner(context.Background(), session)
}

func (s *Server) appendDispatchLocked(session *logicalSession, eventType string, payload []byte) {
	session.sequence++
	frame := new(sessionv1.ConnectResponse)
	frame.SetOpcode(opDispatch)
	frame.SetSequence(session.sequence)
	frame.SetType(eventType)
	frame.SetJsonPayload(string(payload))
	session.replay = append(session.replay, replayEntry{sequence: session.sequence, frame: frame})
	if len(session.replay) > s.svcCtx.Cfg.Node.ReplayLimit() {
		overflow := len(session.replay) - s.svcCtx.Cfg.Node.ReplayLimit()
		session.replayFloor = session.replay[overflow-1].sequence
		session.replay = session.replay[overflow:]
	}
	if session.binding != nil {
		if err := enqueue(session.binding, cloneFrame(frame)); err != nil {
			session.binding.close()
			session.binding = nil
			session.detachedAt = time.Now()
		}
	}
}

func enqueue(binding *binding, frame *sessionv1.ConnectResponse) error {
	select {
	case binding.send <- frame:
		return nil
	case <-binding.done:
		return errors.New("session binding closed")
	default:
		sessionBindingQueueOverflowsTotal.Inc()
		return errors.New("session binding queue full")
	}
}

func sendSessionGatewayFrame(stream sessionv1.SessionService_ConnectServer, frame *sessionv1.ConnectResponse) error {
	if err := stream.Send(frame); err != nil {
		return err
	}
	observeSessionGatewayFrame("gateway_out", proto.Size(frame))
	return nil
}

func newBinding(id string, epoch uint64, queueSize int) *binding {
	return &binding{id: id, epoch: epoch, send: make(chan *sessionv1.ConnectResponse, queueSize), done: make(chan struct{})}
}

func cloneFrame(frame *sessionv1.ConnectResponse) *sessionv1.ConnectResponse {
	cloned := new(sessionv1.ConnectResponse)
	cloned.SetOpcode(frame.GetOpcode())
	cloned.SetSequence(frame.GetSequence())
	cloned.SetType(frame.GetType())
	cloned.SetJsonPayload(frame.GetJsonPayload())
	cloned.SetCloseCode(frame.GetCloseCode())
	cloned.SetCloseReason(frame.GetCloseReason())
	return cloned
}

func (s *Server) addSession(session *logicalSession, snapshots map[int64]*visibilitySnapshot) {
	s.mu.Lock()
	s.sessions[session.id] = session
	addIndex(s.users, session.userID, session)
	for guildID := range session.guilds {
		addIndex(s.guilds, guildID, session)
	}
	s.mu.Unlock()
	s.retainVisibilitySnapshots(session.userID, snapshots)
}

func addIndex(index map[int64]map[*logicalSession]struct{}, id int64, session *logicalSession) {
	set := index[id]
	if set == nil {
		set = make(map[*logicalSession]struct{})
		index[id] = set
	}
	set[session] = struct{}{}
}

func removeIndex(index map[int64]map[*logicalSession]struct{}, id int64, session *logicalSession) {
	delete(index[id], session)
	if len(index[id]) == 0 {
		delete(index, id)
	}
}

func (s *Server) removeSession(ctx context.Context, session *logicalSession) {
	session.mu.Lock()
	if session.binding != nil {
		session.binding.close()
		session.binding = nil
	}
	guildIDs := mapKeys(session.guilds)
	session.mu.Unlock()

	s.mu.Lock()
	if current := s.sessions[session.id]; current != session {
		s.mu.Unlock()
		return
	}
	delete(s.sessions, session.id)
	removeIndex(s.users, session.userID, session)
	for _, guildID := range guildIDs {
		removeIndex(s.guilds, guildID, session)
	}
	s.mu.Unlock()
	s.releaseVisibilitySnapshots(session.userID)
	_ = s.svcCtx.Store.DeleteOwner(ctx, session.id, s.nodeID, s.generation)
	s.removePresence(ctx, session, guildIDs)
	s.refreshAllRoutes(ctx)
}

func (s *Server) refreshOwner(ctx context.Context, session *logicalSession) error {
	return s.svcCtx.Store.SetOwner(ctx, store.Owner{
		SessionID: session.id, NodeID: s.nodeID, Generation: s.generation,
	}, s.svcCtx.Cfg.Node.ResumeTTL())
}

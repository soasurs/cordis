package server

import (
	"context"
	"time"
)

func (s *Server) cleanupDetached(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := time.Now()
		s.mu.RLock()
		sessions := make([]*logicalSession, 0, len(s.sessions))
		for _, session := range s.sessions {
			sessions = append(sessions, session)
		}
		s.mu.RUnlock()
		for _, session := range sessions {
			session.mu.Lock()
			expired := session.binding == nil && !session.detachedAt.IsZero() &&
				now.Sub(session.detachedAt) >= s.svcCtx.Cfg.Node.ResumeTTL()
			session.mu.Unlock()
			if expired {
				s.removeSession(ctx, session)
			}
		}
	}
}

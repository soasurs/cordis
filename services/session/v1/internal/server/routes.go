package server

import (
	"context"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/soasurs/cordis/pkg/sessionregistry"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
)

func (s *Server) refreshNode(ctx context.Context) {
	ticker := time.NewTicker(s.svcCtx.Cfg.Node.HeartbeatInterval())
	defer ticker.Stop()
	for {
		nodeStatus := sessionregistry.StatusReady
		if s.draining.Load() {
			nodeStatus = sessionregistry.StatusDraining
		}
		err := s.registerNode(ctx, nodeStatus)
		if err != nil && ctx.Err() == nil {
			logx.WithContext(ctx).Errorw("register session node", logx.Field("error", err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) registerNode(ctx context.Context, nodeStatus string) error {
	return s.svcCtx.SessionRegistry.Register(ctx, sessionregistry.Node{
		ID: s.nodeID, Generation: s.generation, RPCAddress: s.rpcAddress, Status: nodeStatus,
	}, s.svcCtx.Cfg.Node.NodeTTL())
}

func (s *Server) refreshRoutes(ctx context.Context) {
	ticker := time.NewTicker(s.svcCtx.Cfg.Node.RouteRefreshInterval())
	defer ticker.Stop()
	for {
		s.refreshAllRoutes(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) refreshAllRoutes(ctx context.Context) {
	routes := s.routeSnapshot()
	active := make(map[store.Route]struct{}, len(routes))
	for _, route := range routes {
		active[route] = struct{}{}
	}

	s.routeMu.Lock()
	defer s.routeMu.Unlock()

	detached := make([]store.Route, 0)
	for route := range s.publishedRoutes {
		if _, ok := active[route]; !ok {
			detached = append(detached, route)
		}
	}
	if err := s.svcCtx.Store.DetachRoutes(ctx, s.nodeID, s.generation, detached); err != nil {
		if ctx.Err() == nil {
			logx.WithContext(ctx).Errorw("detach session routes", logx.Field("error", err))
		}
	} else {
		for _, route := range detached {
			delete(s.publishedRoutes, route)
		}
	}

	if err := s.svcCtx.Store.RefreshRoutes(ctx, s.nodeID, s.generation, routes, s.svcCtx.Cfg.Node.RouteTTL()); err != nil && ctx.Err() == nil {
		logx.WithContext(ctx).Errorw("refresh session routes", logx.Field("error", err))
	} else if err == nil {
		for route := range active {
			s.publishedRoutes[route] = struct{}{}
		}
	}
}

func (s *Server) routeSnapshot() []store.Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	routes := make([]store.Route, 0, len(s.users)+len(s.guilds))
	for id := range s.users {
		routes = append(routes, store.Route{Kind: store.RouteUser, ID: id})
	}
	for id := range s.guilds {
		routes = append(routes, store.Route{Kind: store.RouteGuild, ID: id})
	}
	return routes
}

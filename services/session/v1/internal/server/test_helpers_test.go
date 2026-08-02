package server

import (
	"github.com/soasurs/cordis/services/session/v1/config"
	"github.com/soasurs/cordis/services/session/v1/internal/svc"
)

func newTestServer() *Server {
	return newTestServerWithRegistry(&fakeRegistry{})
}

func newTestServerWithRegistry(registry *fakeRegistry) *Server {
	cfg := config.Config{
		Node: config.NodeConfig{
			ID: "session-test", AdvertiseAddress: "127.0.0.1:3006",
			SessionResumeSeconds: 120, MaxReplayEvents: 2048, BindingQueueSize: 4096,
		},
	}
	return New(svc.NewServiceContextWithDependencies(cfg, svc.Dependencies{
		Store:               &fakeStore{},
		SessionRegistry:     registry,
		AuthenticatorClient: fakeAuthenticator{},
		UserClient:          fakeUser{},
		PresenceClient:      fakePresence{},
		GuildClient:         fakeGuild{},
		MessageClient:       new(fakeMessage),
	}))
}

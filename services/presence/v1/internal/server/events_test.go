package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/presence/v1/config"
	"github.com/soasurs/cordis/services/presence/v1/internal/store"
	"github.com/soasurs/cordis/services/presence/v1/internal/svc"
)

type publishedPresenceRecord struct {
	key     string
	payload []byte
}

type fakePublisher struct {
	records []publishedPresenceRecord
}

func (p *fakePublisher) Publish(_ context.Context, key, payload []byte) error {
	p.records = append(p.records, publishedPresenceRecord{key: string(key), payload: append([]byte(nil), payload...)})
	return nil
}

func newTestServerWithPublisher() (presencev1.PresenceServiceServer, *fakeStore, *fakePublisher) {
	fake := &fakeStore{}
	publisher := new(fakePublisher)
	svcCtx := svc.NewServiceContextWithDependencies(config.Config{
		Kafka: config.KafkaConfig{PublishTimeoutMs: 100},
	}, svc.Dependencies{Store: fake, Publisher: publisher})
	return New(svcCtx), fake, publisher
}

func registerRequest(userID int64, status presencev1.PresenceStatus, guildIDs ...int64) *presencev1.RegisterUserSessionRequest {
	req := new(presencev1.RegisterUserSessionRequest)
	req.SetUserId(userID)
	req.SetSessionId("sess-1")
	req.SetGatewayId("gateway-a")
	req.SetGeneration("gen-1")
	req.SetStatus(status)
	req.SetGuildIds(guildIDs)
	return req
}

func TestRegisterPublishesAggregateTransition(t *testing.T) {
	server, fake, publisher := newTestServerWithPublisher()
	// The aggregate was offline before this session arrived.
	fake.presences = []store.UserPresence{{UserID: 601, Status: store.PresenceStatusOffline}}

	_, err := server.RegisterUserSession(context.Background(), registerRequest(601, presencev1.PresenceStatus_PRESENCE_STATUS_ONLINE, 11, 12))
	require.NoError(t, err)

	require.Len(t, publisher.records, 1)
	require.Equal(t, "601", publisher.records[0].key)
	var envelope struct {
		Type string          `json:"t"`
		Data json.RawMessage `json:"d"`
	}
	require.NoError(t, json.Unmarshal(publisher.records[0].payload, &envelope))
	require.Equal(t, realtime.EventPresenceUpdated, envelope.Type)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(envelope.Data, &payload))
	require.Equal(t, `"601"`, string(payload["user_id"]))
	require.JSONEq(t, `["11","12"]`, string(payload["guild_ids"]))
}

func TestRefreshWithUnchangedAggregateStaysSilent(t *testing.T) {
	server, fake, publisher := newTestServerWithPublisher()
	fake.presences = []store.UserPresence{{UserID: 601, Status: store.PresenceStatusOnline}}

	req := new(presencev1.RefreshUserSessionRequest)
	req.SetUserId(601)
	req.SetSessionId("sess-1")
	req.SetGatewayId("gateway-a")
	req.SetGeneration("gen-1")
	req.SetStatus(presencev1.PresenceStatus_PRESENCE_STATUS_ONLINE)
	_, err := server.RefreshUserSession(context.Background(), req)
	require.NoError(t, err)
	require.Empty(t, publisher.records)
}

func TestUpdatePresencePublishesStatusChange(t *testing.T) {
	server, fake, publisher := newTestServerWithPublisher()
	fake.presences = []store.UserPresence{{UserID: 601, Status: store.PresenceStatusOnline}}

	req := new(presencev1.UpdateUserPresenceRequest)
	req.SetUserId(601)
	req.SetSessionId("sess-1")
	req.SetStatus(presencev1.PresenceStatus_PRESENCE_STATUS_DND)
	req.SetGuildIds([]int64{11})
	_, err := server.UpdateUserPresence(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, publisher.records, 1)
}

func TestRemoveUserSessionPublishesOffline(t *testing.T) {
	server, fake, publisher := newTestServerWithPublisher()
	// First snapshot (before removal) is online, second (after) is offline.
	fake.presenceSequence = [][]store.UserPresence{
		{{UserID: 601, Status: store.PresenceStatusOnline}},
		{{UserID: 601, Status: store.PresenceStatusOffline}},
	}

	req := new(presencev1.RemoveUserSessionRequest)
	req.SetUserId(601)
	req.SetSessionId("sess-1")
	req.SetGuildIds([]int64{11})
	_, err := server.RemoveUserSession(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, publisher.records, 1)

	var envelope struct {
		Type string `json:"t"`
		Data struct {
			Status int32 `json:"status"`
		} `json:"d"`
	}
	require.NoError(t, json.Unmarshal(publisher.records[0].payload, &envelope))
	require.Equal(t, int32(store.PresenceStatusOffline), envelope.Data.Status)
}

func TestRemoveUserSessionWithoutTransitionStaysSilent(t *testing.T) {
	server, fake, publisher := newTestServerWithPublisher()
	// Another device keeps the user online across the removal.
	fake.presences = []store.UserPresence{{UserID: 601, Status: store.PresenceStatusOnline}}

	req := new(presencev1.RemoveUserSessionRequest)
	req.SetUserId(601)
	req.SetSessionId("sess-1")
	_, err := server.RemoveUserSession(context.Background(), req)
	require.NoError(t, err)
	require.Empty(t, publisher.records)
}

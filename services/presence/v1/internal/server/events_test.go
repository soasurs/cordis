package server

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

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
	mu           sync.Mutex
	records      []publishedPresenceRecord
	publishCalls int
	publishStart chan struct{}
	publishBlock chan struct{}
}

func (p *fakePublisher) Publish(ctx context.Context, key, payload []byte) error {
	p.mu.Lock()
	isFirst := p.publishCalls == 0
	p.publishCalls++
	p.mu.Unlock()
	if isFirst && p.publishStart != nil {
		select {
		case p.publishStart <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.publishBlock:
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.records = append(p.records, publishedPresenceRecord{key: string(key), payload: append([]byte(nil), payload...)})
	return nil
}

func newTestServerWithPublisher() (presencev1.PresenceServiceServer, *fakeStore, *fakePublisher) {
	fake := &fakeStore{}
	publisher := new(fakePublisher)
	svcCtx := svc.NewServiceContextWithDependencies(config.Config{
		Kafka: config.KafkaConfig{PublishTimeoutMs: 100},
	}, svc.Dependencies{Store: fake, Snowflake: newTestSnowflake(), Publisher: publisher})
	return New(svcCtx), fake, publisher
}

func registerRequest(userID int64, status presencev1.PresenceStatus, guildIDs ...int64) *presencev1.RegisterUserSessionRequest {
	req := new(presencev1.RegisterUserSessionRequest)
	req.SetUserId(userID)
	req.SetSessionId("sess-1")
	req.SetGatewayId("gateway-a")
	req.SetGeneration("gen-1")
	req.SetInitialStatus(status)
	req.SetClientState(presencev1.ClientState_CLIENT_STATE_FOREGROUND)
	req.SetGuildIds(guildIDs)
	return req
}

func TestRegisterPublishesAggregateTransition(t *testing.T) {
	server, fake, publisher := newTestServerWithPublisher()
	// The aggregate was offline before this session arrived.
	fake.snapshot = &store.UserPresence{UserID: 601, Status: store.PresenceStatusOffline, Version: 100}

	_, err := server.RegisterUserSession(context.Background(), registerRequest(601, presencev1.PresenceStatus_PRESENCE_STATUS_ONLINE, 11, 12))
	require.NoError(t, err)

	require.Len(t, publisher.records, 1)
	require.Equal(t, "601", publisher.records[0].key)
	var envelope struct {
		Type           string          `json:"t"`
		Data           json.RawMessage `json:"d"`
		IdempotencyKey string          `json:"idempotency_key"`
	}
	require.NoError(t, json.Unmarshal(publisher.records[0].payload, &envelope))
	require.Equal(t, realtime.EventPresenceUpdated, envelope.Type)
	require.NotEqual(t, "0", envelope.IdempotencyKey)
	require.NotEmpty(t, envelope.IdempotencyKey)
	var payload map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(envelope.Data, &payload))
	require.Equal(t, `"601"`, string(payload["user_id"]))
	require.JSONEq(t, `["11","12"]`, string(payload["guild_ids"]))
	require.NotEqual(t, `"100"`, string(payload["version"]))
	require.Equal(t, string(payload["version"]), `"`+envelope.IdempotencyKey+`"`)
	require.Equal(t, fake.snapshot.Version, mustParseVersion(t, envelope.IdempotencyKey))
}

func mustParseVersion(t *testing.T, value string) int64 {
	t.Helper()
	version, err := strconv.ParseInt(value, 10, 64)
	require.NoError(t, err)
	return version
}

func TestRefreshWithUnchangedAggregateStaysSilent(t *testing.T) {
	server, fake, publisher := newTestServerWithPublisher()
	fake.snapshot = &store.UserPresence{UserID: 601, Status: store.PresenceStatusOnline, Version: 100}
	fake.presences = []store.UserPresence{{
		UserID: 601, Status: store.PresenceStatusOnline,
		Sessions: []store.UserSession{{UserID: 601, SessionID: "sess-1"}},
	}}

	req := new(presencev1.RefreshUserSessionRequest)
	req.SetUserId(601)
	req.SetSessionId("sess-1")
	req.SetGatewayId("gateway-a")
	req.SetGeneration("gen-1")
	req.SetClientState(presencev1.ClientState_CLIENT_STATE_FOREGROUND)
	_, err := server.RefreshUserSession(context.Background(), req)
	require.NoError(t, err)
	require.Empty(t, publisher.records)
}

func TestUpdatePresencePublishesStatusChange(t *testing.T) {
	server, fake, publisher := newTestServerWithPublisher()
	fake.snapshot = &store.UserPresence{UserID: 601, Status: store.PresenceStatusOnline, Version: 100}

	req := new(presencev1.UpdateUserPresenceRequest)
	req.SetUserId(601)
	req.SetSessionId("sess-1")
	req.SetStatus(presencev1.PresenceStatus_PRESENCE_STATUS_DND)
	req.SetClientState(presencev1.ClientState_CLIENT_STATE_FOREGROUND)
	req.SetGuildIds([]int64{11})
	_, err := server.UpdateUserPresence(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, publisher.records, 2)
	var types []string
	for _, record := range publisher.records {
		var envelope struct {
			Type string `json:"t"`
		}
		require.NoError(t, json.Unmarshal(record.payload, &envelope))
		types = append(types, envelope.Type)
	}
	require.Equal(t, []string{
		realtime.EventPresencePreferenceUpdated, realtime.EventPresenceUpdated,
	}, types)
}

func TestRemoveUserSessionPublishesOffline(t *testing.T) {
	server, fake, publisher := newTestServerWithPublisher()
	fake.snapshot = &store.UserPresence{UserID: 601, Status: store.PresenceStatusOnline, LastSeenAt: 123, Version: 100}
	fake.presenceSequence = [][]store.UserPresence{{{UserID: 601, Status: store.PresenceStatusOffline}}}

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
	require.Equal(t, int64(123), fake.snapshot.LastSeenAt)
}

func TestRemoveUserSessionWithoutTransitionStaysSilent(t *testing.T) {
	server, fake, publisher := newTestServerWithPublisher()
	// Another device keeps the user online across the removal.
	fake.snapshot = &store.UserPresence{UserID: 601, Status: store.PresenceStatusOnline, Version: 100}
	fake.presences = []store.UserPresence{{UserID: 601, Status: store.PresenceStatusOnline}}

	req := new(presencev1.RemoveUserSessionRequest)
	req.SetUserId(601)
	req.SetSessionId("sess-1")
	_, err := server.RemoveUserSession(context.Background(), req)
	require.NoError(t, err)
	require.Empty(t, publisher.records)
}

func TestConcurrentUpdatesPublishInMutationOrder(t *testing.T) {
	server, fake, publisher := newTestServerWithPublisher()
	fake.snapshot = &store.UserPresence{UserID: 601, Status: store.PresenceStatusOnline, Version: 100}
	fake.presences = []store.UserPresence{{
		UserID: 601, Status: store.PresenceStatusOnline,
		Sessions: []store.UserSession{{UserID: 601, SessionID: "sess-1"}},
	}}
	publisher.publishStart = make(chan struct{}, 1)
	publisher.publishBlock = make(chan struct{})

	update := func(status presencev1.PresenceStatus) error {
		req := new(presencev1.UpdateUserPresenceRequest)
		req.SetUserId(601)
		req.SetSessionId("sess-1")
		req.SetStatus(status)
		req.SetClientState(presencev1.ClientState_CLIENT_STATE_FOREGROUND)
		_, err := server.UpdateUserPresence(t.Context(), req)
		return err
	}

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- update(presencev1.PresenceStatus_PRESENCE_STATUS_DND)
	}()
	select {
	case <-publisher.publishStart:
	case <-time.After(time.Second):
		t.Fatal("first publish did not start")
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- update(presencev1.PresenceStatus_PRESENCE_STATUS_IDLE)
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second update completed before the first publish: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(publisher.publishBlock)
	require.NoError(t, <-firstDone)
	require.NoError(t, <-secondDone)

	publisher.mu.Lock()
	records := append([]publishedPresenceRecord(nil), publisher.records...)
	publisher.mu.Unlock()
	require.Len(t, records, 4)
	require.Equal(t, []int32{int32(store.PresenceStatusDND), int32(store.PresenceStatusIdle)}, publishedStatuses(t, records))
}

func publishedStatuses(t *testing.T, records []publishedPresenceRecord) []int32 {
	t.Helper()
	statuses := make([]int32, 0, len(records))
	for _, record := range records {
		var envelope struct {
			Type string          `json:"t"`
			Data json.RawMessage `json:"d"`
		}
		require.NoError(t, json.Unmarshal(record.payload, &envelope))
		if envelope.Type != realtime.EventPresenceUpdated {
			continue
		}
		var payload struct {
			Status int32 `json:"status"`
		}
		require.NoError(t, json.Unmarshal(envelope.Data, &payload))
		statuses = append(statuses, payload.Status)
	}
	return statuses
}

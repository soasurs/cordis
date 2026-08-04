//go:build integration

package server

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/pkg/database"
	cordiskafka "github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/pkg/outbox/relay"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/user/v1/config"
	usermigrations "github.com/soasurs/cordis/services/user/v1/db/migrations"
	"github.com/soasurs/cordis/services/user/v1/internal/eventoutbox"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
	"github.com/soasurs/cordis/services/user/v1/internal/svc"
)

func TestUserOutboxForwardsToKafka(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	kafkaEnv := testkit.StartKafka(t)
	migrationDB, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, migrationDB.Close()) })
	db, err := database.NewPostgresPool(t.Context(), database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, migration.Apply(t.Context(), migrationDB, usermigrations.Files))

	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	topic := "cordis.integration.user." + runID
	topicProducer, err := kgo.NewClient(kgo.SeedBrokers(kafkaEnv.Address))
	require.NoError(t, err)
	testkit.CreateKafkaTopic(t, topicProducer, topic)
	topicProducer.Close()

	producer, err := cordiskafka.NewProducer(cordiskafka.ProducerConfig{Seeds: []string{kafkaEnv.Address}})
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	publisher := cordiskafka.NewPublisher(producer, topic)

	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(kafkaEnv.Address),
		kgo.ConsumerGroup("cordis.integration.user-consumer."+runID),
		kgo.ConsumeTopics(topic),
	)
	require.NoError(t, err)
	t.Cleanup(consumer.Close)

	node, err := snowflake.New()
	require.NoError(t, err)
	codec, err := cursor.NewCodec("test-cursor-secret-at-least-32-bytes!")
	require.NoError(t, err)
	userStore := store.New(db)
	service := New(svc.NewServiceContextWithDependencies(config.Config{
		Kafka: config.KafkaConfig{Topic: topic},
	}, svc.Dependencies{
		Store:       userStore,
		Snowflake:   node,
		Cursors:     codec,
		MediaClient: &fakeMediaClient{},
	}))

	relayCtx, cancelRelay := context.WithCancel(t.Context())
	userRelay, err := relay.New(relay.Config{
		DB:            db,
		Tables:        eventoutbox.Tables(),
		Publisher:     publisher,
		Namespace:     "cordis.integration.user.outbox." + runID,
		NotifyChannel: eventoutbox.UserNotifyChannel,
		ListenerDSN:   postgres.DSN,
		Workers:       2,
		BatchSize:     1,
		PollInterval:  time.Minute,
		TimeSlice:     time.Millisecond,
		BackoffMin:    10 * time.Millisecond,
		BackoffMax:    100 * time.Millisecond,
	})
	require.NoError(t, err)
	relayDone := make(chan struct{})
	go func() {
		defer close(relayDone)
		_ = userRelay.Run(relayCtx)
	}()
	t.Cleanup(func() {
		cancelRelay()
		<-relayDone
	})

	_, err = userStore.CreateUser(t.Context(), 3001, "alice@example.com")
	require.NoError(t, err)
	_, err = userStore.CreateUserProfile(t.Context(), 3001, "alice", "Alice")
	require.NoError(t, err)
	_, err = userStore.CreateUser(t.Context(), 3002, "bob@example.com")
	require.NoError(t, err)
	_, err = userStore.CreateUserProfile(t.Context(), 3002, "bob", "Bob")
	require.NoError(t, err)

	updateReq := new(userv1.UpdateUsernameRequest)
	updateReq.SetUserId(3001)
	updateReq.SetUsername("alice_new")
	_, err = service.UpdateUsername(t.Context(), updateReq)
	require.NoError(t, err)

	readCtx, cancelRead := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancelRead()
	records := consumer.PollRecords(readCtx, 1)
	require.Empty(t, records.Errors())
	require.Len(t, records.Records(), 1)
	require.Equal(t, "3001", string(records.Records()[0].Key))
	var profileEnvelope eventEnvelope[userProfilePayload]
	require.NoError(t, json.Unmarshal(records.Records()[0].Value, &profileEnvelope))
	require.Equal(t, EventTypeUserProfileUpdated, profileEnvelope.Type)
	require.Equal(t, "alice_new", profileEnvelope.Data.Username)
	require.NotZero(t, profileEnvelope.StreamSequence)

	friendReq := new(userv1.SendFriendRequestRequest)
	friendReq.SetUserId(3001)
	friendReq.SetTargetId(3002)
	_, err = service.SendFriendRequest(t.Context(), friendReq)
	require.NoError(t, err)

	friendCtx, cancelFriend := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancelFriend()
	var friendRecords []*kgo.Record
	for len(friendRecords) < 2 && friendCtx.Err() == nil {
		records = consumer.PollRecords(friendCtx, 2-len(friendRecords))
		require.Empty(t, records.Errors())
		friendRecords = append(friendRecords, records.Records()...)
	}
	require.Len(t, friendRecords, 2)
	keys := make(map[string]bool, 2)
	for _, record := range friendRecords {
		keys[string(record.Key)] = true
		var relEnvelope eventEnvelope[relationshipPayload]
		require.NoError(t, json.Unmarshal(record.Value, &relEnvelope))
		require.Equal(t, EventTypeRelationshipUpdated, relEnvelope.Type)
		require.NotZero(t, relEnvelope.StreamSequence)
	}
	require.Equal(t, map[string]bool{"3001": true, "3002": true}, keys)
}

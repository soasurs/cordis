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

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/pkg/database"
	cordiskafka "github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/pkg/outbox/relay"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/message/v1/config"
	messagemigrations "github.com/soasurs/cordis/services/message/v1/db/migrations"
	"github.com/soasurs/cordis/services/message/v1/internal/eventoutbox"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
)

func TestCreateMessageOutboxForwardsToKafka(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	kafkaEnv := testkit.StartKafka(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migration.Apply(t.Context(), db, messagemigrations.Files))

	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	topic := "cordis.integration.message." + runID
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
		kgo.ConsumerGroup("cordis.integration.message-consumer."+runID),
		kgo.ConsumeTopics(topic),
	)
	require.NoError(t, err)
	t.Cleanup(consumer.Close)

	node, err := snowflake.New()
	require.NoError(t, err)
	codec, err := cursor.NewCodec("test-cursor-secret-at-least-32-bytes!")
	require.NoError(t, err)
	messageStore := store.New(db)
	service := New(svc.NewServiceContextWithDependencies(config.Config{
		Kafka: config.KafkaConfig{Topic: topic},
	}, svc.Dependencies{
		Store:       messageStore,
		Snowflake:   node,
		Cursors:     codec,
		GuildClient: &fakeGuildClient{},
		UserClient:  newFakeUserClient(),
		MediaClient: &unusedMediaClient{},
	}))

	relayCtx, cancelRelay := context.WithCancel(t.Context())
	messageRelay, err := relay.New(relay.Config{
		DB:            db,
		Tables:        eventoutbox.MessageTables(),
		Publisher:     publisher,
		Namespace:     "cordis.integration.message.outbox." + runID,
		NotifyChannel: eventoutbox.MessageNotifyChannel,
		ListenerDSN:   postgres.DSN,
		Workers:       2,
		BatchSize:     10,
		PollInterval:  time.Minute,
		TimeSlice:     time.Second,
		BackoffMin:    10 * time.Millisecond,
		BackoffMax:    100 * time.Millisecond,
	})
	require.NoError(t, err)
	relayDone := make(chan struct{})
	go func() {
		defer close(relayDone)
		_ = messageRelay.Run(relayCtx)
	}()
	t.Cleanup(func() {
		cancelRelay()
		<-relayDone
	})

	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(2001)
	req.SetAuthorId(3001)
	req.SetContent("hello")
	req.SetIdempotencyKey("message-intent-1")
	created, err := service.CreateMessage(t.Context(), req)
	require.NoError(t, err)
	retried, err := service.CreateMessage(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, created.GetMessage().GetId(), retried.GetMessage().GetId())

	readCtx, cancelRead := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancelRead()
	records := consumer.PollRecords(readCtx, 1)
	require.Empty(t, records.Errors())
	require.Len(t, records.Records(), 1)
	var envelope eventEnvelope[messagePayload]
	require.NoError(t, json.Unmarshal(records.Records()[0].Value, &envelope))
	require.Equal(t, EventTypeMessageCreated, envelope.Type)
	require.Equal(t, "2001", string(records.Records()[0].Key))
	require.Equal(t, "9001", envelope.Data.GuildID)
	require.Equal(t, strconv.FormatInt(created.GetMessage().GetId(), 10), envelope.Data.MessageID)
	require.NotZero(t, envelope.StreamSequence)

	extraCtx, cancelExtra := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancelExtra()
	extra := consumer.PollRecords(extraCtx, 1)
	require.Empty(t, extra.Records(), "idempotent retry must not enqueue another event")

	require.NoError(t, messageStore.CreateDmChannel(t.Context(), &model.DmChannel{
		ID: 4001, UserLo: 3001, UserHi: 3002, CreatedAt: 1,
	}))
	dmReq := new(messagev1.CreateMessageRequest)
	dmReq.SetChannelId(4001)
	dmReq.SetAuthorId(3001)
	dmReq.SetContent("hello dm")
	_, err = service.CreateMessage(t.Context(), dmReq)
	require.NoError(t, err)

	dmCtx, cancelDM := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancelDM()
	var dmRecords []*kgo.Record
	for len(dmRecords) < 2 && dmCtx.Err() == nil {
		records = consumer.PollRecords(dmCtx, 2-len(dmRecords))
		require.Empty(t, records.Errors())
		dmRecords = append(dmRecords, records.Records()...)
	}
	require.Len(t, dmRecords, 2)
	createdRecipients := make(map[string]bool)
	for _, record := range dmRecords {
		var dmEnvelope eventEnvelope[messagePayload]
		require.NoError(t, json.Unmarshal(record.Value, &dmEnvelope))
		require.Equal(t, EventTypeMessageCreated, dmEnvelope.Type)
		require.Equal(t, "4001", string(record.Key))
		createdRecipients[dmEnvelope.Data.UserID] = true
	}
	require.Equal(t, map[string]bool{"3001": true, "3002": true}, createdRecipients)
}

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
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/message/v1/config"
	messagemigrations "github.com/soasurs/cordis/services/message/v1/db/migrations"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
)

func TestCreateMessagePersistsAndPublishesToKafka(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	kafka := testkit.StartKafka(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migration.Apply(t.Context(), db, messagemigrations.Files))

	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	topic := "cordis.integration.message." + runID
	producer, err := kgo.NewClient(kgo.SeedBrokers(kafka.Address))
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	testkit.CreateKafkaTopic(t, producer, topic)
	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(kafka.Address),
		kgo.ConsumerGroup("cordis.integration.message-consumer."+runID),
		kgo.ConsumeTopics(topic),
	)
	require.NoError(t, err)
	t.Cleanup(consumer.Close)

	node, err := snowflake.New()
	require.NoError(t, err)
	messageStore := store.New(db)
	service := New(svc.NewServiceContextWithDependencies(config.Config{
		Kafka: config.KafkaConfig{Topic: topic, PublishTimeoutMs: 5000},
	}, svc.Dependencies{
		Store:       messageStore,
		Snowflake:   node,
		Kafka:       producer,
		GuildClient: &fakeGuildClient{},
		UserClient:  newFakeUserClient(),
	}))

	req := new(messagev1.CreateMessageRequest)
	req.SetChannelId(2001)
	req.SetAuthorId(3001)
	req.SetContent("hello")
	created, err := service.CreateMessage(t.Context(), req)
	require.NoError(t, err)

	readCtx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	records := consumer.PollRecords(readCtx, 2)
	require.Empty(t, records.Errors())
	require.Len(t, records.Records(), 2)
	var foundCreated, foundRead bool
	for _, record := range records.Records() {
		var envelope eventEnvelope[messagePayload]
		require.NoError(t, json.Unmarshal(record.Value, &envelope))
		switch envelope.Type {
		case EventTypeMessageCreated:
			foundCreated = true
			require.Equal(t, "9001", string(record.Key))
			require.Equal(t, "9001", envelope.Data.GuildID)
			require.Equal(t, strconv.FormatInt(created.GetMessage().GetId(), 10), envelope.Data.MessageID)
		case EventTypeMessageReadUpdated:
			foundRead = true
			require.Equal(t, "3001", string(record.Key))
		}
	}
	require.True(t, foundCreated)
	require.True(t, foundRead)

	require.NoError(t, messageStore.CreateDmChannel(t.Context(), &model.DmChannel{
		ID: 4001, UserLo: 3001, UserHi: 3002, CreatedAt: 1,
	}))
	dmReq := new(messagev1.CreateMessageRequest)
	dmReq.SetChannelId(4001)
	dmReq.SetAuthorId(3001)
	dmReq.SetContent("hello dm")
	_, err = service.CreateMessage(t.Context(), dmReq)
	require.NoError(t, err)

	var dmRecords []*kgo.Record
	for len(dmRecords) < 3 && readCtx.Err() == nil {
		records = consumer.PollRecords(readCtx, 3-len(dmRecords))
		require.Empty(t, records.Errors())
		dmRecords = append(dmRecords, records.Records()...)
	}
	require.Len(t, dmRecords, 3)
	createdRecipients := make(map[string]bool)
	for _, record := range dmRecords {
		var dmEnvelope eventEnvelope[messagePayload]
		require.NoError(t, json.Unmarshal(record.Value, &dmEnvelope))
		if dmEnvelope.Type == EventTypeMessageCreated {
			createdRecipients[string(record.Key)] = true
			continue
		}
		require.Equal(t, EventTypeMessageReadUpdated, dmEnvelope.Type)
		require.Equal(t, "3001", string(record.Key))
	}
	require.Equal(t, map[string]bool{"3001": true, "3002": true}, createdRecipients)
}

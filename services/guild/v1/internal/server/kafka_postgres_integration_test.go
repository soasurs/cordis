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

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/guild/v1/config"
	guildmigrations "github.com/soasurs/cordis/services/guild/v1/db/migrations"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
	"github.com/soasurs/cordis/services/guild/v1/internal/svc"
)

type unusedMediaClient struct {
	mediav1.MediaServiceClient
}

func TestCreateGuildPersistsAndPublishesToKafka(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	kafka := testkit.StartKafka(t)
	db, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, migration.Apply(t.Context(), db, guildmigrations.Files))

	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	topic := "cordis.integration.guild." + runID
	producer, err := kgo.NewClient(kgo.SeedBrokers(kafka.Address))
	require.NoError(t, err)
	t.Cleanup(producer.Close)
	testkit.CreateKafkaTopic(t, producer, topic)
	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(kafka.Address),
		kgo.ConsumerGroup("cordis.integration.guild-consumer."+runID),
		kgo.ConsumeTopics(topic),
	)
	require.NoError(t, err)
	t.Cleanup(consumer.Close)

	node, err := snowflake.New()
	require.NoError(t, err)
	service := New(svc.NewServiceContextWithDependencies(config.Config{
		Kafka: config.KafkaConfig{Topic: topic, PublishTimeoutMs: 5000},
	}, svc.Dependencies{
		Store:       store.New(db),
		Snowflake:   node,
		Kafka:       producer,
		UserClient:  &fakeUserClient{},
		MediaClient: &unusedMediaClient{},
	}))

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")
	created, err := service.CreateGuild(t.Context(), req)
	require.NoError(t, err)

	readCtx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	records := consumer.PollRecords(readCtx, 1)
	require.Empty(t, records.Errors())
	require.Len(t, records.Records(), 1)
	record := records.Records()[0]
	require.Equal(t, strconv.FormatInt(created.GetGuild().GetId(), 10), string(record.Key))

	var envelope eventEnvelope[guildPayload]
	require.NoError(t, json.Unmarshal(record.Value, &envelope))
	require.Equal(t, EventTypeGuildCreated, envelope.Type)
	require.Equal(t, strconv.FormatInt(created.GetGuild().GetId(), 10), envelope.Data.ID)
	var revisionEnvelope struct {
		Data struct {
			AccessRevision int64 `json:"access_revision"`
		} `json:"d"`
	}
	require.NoError(t, json.Unmarshal(record.Value, &revisionEnvelope))
	require.GreaterOrEqual(t, revisionEnvelope.Data.AccessRevision, int64(3))
}

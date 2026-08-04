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
	cordiskafka "github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/pkg/outbox/relay"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/guild/v1/config"
	guildmigrations "github.com/soasurs/cordis/services/guild/v1/db/migrations"
	"github.com/soasurs/cordis/services/guild/v1/internal/eventoutbox"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
	"github.com/soasurs/cordis/services/guild/v1/internal/svc"
)

type unusedMediaClient struct {
	mediav1.MediaServiceClient
}

func TestCreateGuildPersistsAndPublishesToKafka(t *testing.T) {
	postgres := testkit.StartPostgres(t)
	kafka := testkit.StartKafka(t)
	migrationDB, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, migrationDB.Close()) })
	db, err := database.NewPostgresPool(t.Context(), database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, migration.Apply(t.Context(), migrationDB, guildmigrations.Files))

	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	topic := "cordis.integration.guild." + runID
	topicClient, err := kgo.NewClient(kgo.SeedBrokers(kafka.Address))
	require.NoError(t, err)
	testkit.CreateKafkaTopic(t, topicClient, topic)
	topicClient.Close()
	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(kafka.Address),
		kgo.ConsumerGroup("cordis.integration.guild-consumer."+runID),
		kgo.ConsumeTopics(topic),
	)
	require.NoError(t, err)
	t.Cleanup(consumer.Close)

	node, err := snowflake.New()
	require.NoError(t, err)
	guildStore := store.New(db)
	service := New(svc.NewServiceContextWithDependencies(config.Config{
		Kafka: config.KafkaConfig{Topic: topic},
	}, svc.Dependencies{
		Store:       guildStore,
		Snowflake:   node,
		Cursors:     testCursorCodec(t),
		UserClient:  &fakeUserClient{},
		MediaClient: &unusedMediaClient{},
	}))

	relayProducer, err := cordiskafka.NewProducer(cordiskafka.ProducerConfig{Seeds: []string{kafka.Address}})
	require.NoError(t, err)
	t.Cleanup(relayProducer.Close)
	publisher := cordiskafka.NewPublisher(relayProducer, topic)
	relayCtx, cancelRelay := context.WithCancel(t.Context())
	guildRelay, err := relay.New(relay.Config{
		DB:            db,
		Tables:        eventoutbox.Tables(),
		Publisher:     publisher,
		Namespace:     "cordis.integration.guild.outbox." + runID,
		NotifyChannel: eventoutbox.GuildNotifyChannel,
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
		_ = guildRelay.Run(relayCtx)
	}()
	t.Cleanup(func() {
		cancelRelay()
		<-relayDone
	})

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")
	req.SetIdempotencyKey("guild-intent-1")
	created, err := service.CreateGuild(t.Context(), req)
	require.NoError(t, err)
	retried, err := service.CreateGuild(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, created.GetGuild().GetId(), retried.GetGuild().GetId(), "same-key retry must return the same guild")
	channels, err := guildStore.ListGuildChannels(t.Context(), created.GetGuild().GetId())
	require.NoError(t, err)
	require.Len(t, channels, 4)
	require.Equal(t, defaultTextCategoryName, channels[0].Name)
	require.Equal(t, channels[0].ID, channels[1].ParentID)
	require.Equal(t, defaultVoiceCategoryName, channels[2].Name)
	require.Equal(t, channels[2].ID, channels[3].ParentID)

	readCtx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	records := consumer.PollRecords(readCtx, 10)
	require.Empty(t, records.Errors())
	require.Len(t, records.Records(), 1, "the retry must not republish the creation event")
	record := records.Records()[0]
	require.Equal(t, strconv.FormatInt(created.GetGuild().GetId(), 10), string(record.Key))

	var envelope eventEnvelope[guildPayload]
	require.NoError(t, json.Unmarshal(record.Value, &envelope))
	require.Equal(t, EventTypeGuildCreated, envelope.Type)
	require.Equal(t, strconv.FormatInt(created.GetGuild().GetId(), 10), envelope.Data.ID)
	idempotencyKey, err := strconv.ParseInt(envelope.IdempotencyKey, 10, 64)
	require.NoError(t, err)
	require.Positive(t, idempotencyKey)
	var revisionEnvelope struct {
		Data struct {
			AccessRevision int64 `json:"access_revision"`
		} `json:"d"`
	}
	require.NoError(t, json.Unmarshal(record.Value, &revisionEnvelope))
	require.GreaterOrEqual(t, revisionEnvelope.Data.AccessRevision, int64(7))
}

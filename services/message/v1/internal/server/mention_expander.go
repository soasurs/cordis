package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/core/logx"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
)

const (
	mentionExpandBatchSize   = 10_000
	mentionExpandRetryMin    = 100 * time.Millisecond
	mentionExpandRetryMax    = 5 * time.Second
	mentionExpandMaxAttempts = 8
)

// mentionExpander consumes message created/updated events and materializes
// role and @everyone mention targets into message_mentions (source 2) so
// read-state mention counts include expanded mentions.
type mentionExpander struct {
	consumer *kgo.Client
	store    interface {
		GetMessage(ctx context.Context, messageID int64) (*model.Message, error)
		UpsertExpandedMessageMentions(ctx context.Context, messageID int64, userIDs []int64) error
	}
	guild guildv1.GuildServiceClient
}

// NewMentionExpander creates the expansion worker consumer, or returns a nil
// expander when Kafka is not configured.
func NewMentionExpander(svcCtx *svc.ServiceContext) (*mentionExpander, error) {
	if len(svcCtx.Cfg.Kafka.Seeds) == 0 {
		return nil, nil
	}
	consumer, err := kgo.NewClient(
		kgo.SeedBrokers(svcCtx.Cfg.Kafka.Seeds...),
		kgo.ConsumerGroup(svcCtx.Cfg.Kafka.MentionsConsumerGroup),
		kgo.ConsumeTopics(svcCtx.Cfg.Kafka.Topic),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return nil, err
	}
	return &mentionExpander{
		consumer: consumer,
		store:    svcCtx.Store,
		guild:    svcCtx.GuildClient,
	}, nil
}

func (e *mentionExpander) Close() {
	e.consumer.Close()
}

// Run polls the message event topic until ctx is cancelled, expanding each
// created/updated event that carries role or @everyone mentions.
func (e *mentionExpander) Run(ctx context.Context) {
	for {
		fetches := e.consumer.PollFetches(ctx)
		if ctx.Err() != nil {
			return
		}
		for _, fetchErr := range fetches.Errors() {
			logx.WithContext(ctx).Errorw("poll mention expansion event",
				logx.Field("topic", fetchErr.Topic),
				logx.Field("partition", fetchErr.Partition),
				logx.Field("error", fetchErr.Err),
			)
		}
		fetches.EachPartition(func(ftp kgo.FetchTopicPartition) {
			ftp.EachRecord(func(record *kgo.Record) {
				if err := e.handleRecord(ctx, record); err != nil {
					e.retryRecord(ctx, record)
					return
				}
				if err := e.consumer.CommitRecords(ctx, record); err != nil && ctx.Err() == nil {
					logx.WithContext(ctx).Errorw("commit mention expansion event", logx.Field("error", err))
				}
			})
		})
	}
}

func (e *mentionExpander) retryRecord(ctx context.Context, record *kgo.Record) {
	delay := mentionExpandRetryMin
	for attempt := 1; ctx.Err() == nil && attempt <= mentionExpandMaxAttempts; attempt++ {
		logx.WithContext(ctx).Errorw("retry mention expansion event",
			logx.Field("topic", record.Topic),
			logx.Field("partition", record.Partition),
			logx.Field("offset", record.Offset),
			logx.Field("attempt", attempt),
			logx.Field("retry_after", delay),
		)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if err := e.handleRecord(ctx, record); err == nil {
			if err := e.consumer.CommitRecords(ctx, record); err != nil && ctx.Err() == nil {
				logx.WithContext(ctx).Errorw("commit retried mention expansion event", logx.Field("error", err))
			}
			return
		}
		delay = min(delay*2, mentionExpandRetryMax)
	}
	logx.WithContext(ctx).Errorw("drop mention expansion event after retries",
		logx.Field("topic", record.Topic),
		logx.Field("partition", record.Partition),
		logx.Field("offset", record.Offset),
	)
	if err := e.consumer.CommitRecords(ctx, record); err != nil && ctx.Err() == nil {
		logx.WithContext(ctx).Errorw("commit dropped mention expansion event", logx.Field("error", err))
	}
}

func (e *mentionExpander) handleRecord(ctx context.Context, record *kgo.Record) error {
	var envelope eventEnvelope[messagePayload]
	if err := json.Unmarshal(record.Value, &envelope); err != nil {
		// Malformed events cannot be expanded; treat them as permanent.
		logx.WithContext(ctx).Errorw("drop malformed message event", logx.Field("error", err))
		return nil
	}
	if envelope.Type != EventTypeMessageCreated && envelope.Type != EventTypeMessageUpdated {
		return nil
	}
	payload := envelope.Data
	if len(payload.MentionRoleIDs) == 0 && !payload.MentionEveryone {
		return nil
	}
	if payload.GuildID == "" || payload.ChannelID == "" || payload.MessageID == "" {
		return nil
	}
	messageID, err := strconv.ParseInt(payload.MessageID, 10, 64)
	if err != nil {
		return nil
	}
	channelID, err := strconv.ParseInt(payload.ChannelID, 10, 64)
	if err != nil {
		return nil
	}
	guildID, err := strconv.ParseInt(payload.GuildID, 10, 64)
	if err != nil || guildID <= 0 {
		return nil
	}
	roleIDs := parseIDStrings(payload.MentionRoleIDs)
	return e.expand(ctx, messageID, channelID, guildID, payload.Revision, roleIDs, payload.MentionEveryone)
}

func (e *mentionExpander) expand(
	ctx context.Context,
	messageID, channelID, guildID, revision int64,
	roleIDs []int64,
	everyone bool,
) error {
	message, err := e.store.GetMessage(ctx, messageID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if message.ChannelID != channelID || message.Revision != revision {
		// The event is stale relative to the stored message (edited or
		// deleted since publish); a newer event will drive the expansion.
		return nil
	}

	req := new(guildv1.ListGuildMentionTargetsRequest)
	req.SetGuildId(guildID)
	req.SetActorUserId(message.AuthorID)
	req.SetChannelId(channelID)
	req.SetRoleIds(roleIDs)
	req.SetEveryone(everyone)

	var targets []int64
	for {
		resp, err := e.guild.ListGuildMentionTargets(ctx, req)
		if err != nil {
			return err
		}
		targets = append(targets, resp.GetUserIds()...)
		if !resp.HasNextCursor() {
			break
		}
		req.SetCursor(resp.GetNextCursor())
	}
	for start := 0; start < len(targets); start += mentionExpandBatchSize {
		end := min(start+mentionExpandBatchSize, len(targets))
		if err := e.store.UpsertExpandedMessageMentions(ctx, messageID, targets[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func parseIDStrings(values []string) []int64 {
	ids := make([]int64, 0, len(values))
	for _, value := range values {
		id, err := strconv.ParseInt(value, 10, 64)
		if err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

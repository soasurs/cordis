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
	"github.com/soasurs/cordis/pkg/kafka/partitionconsumer"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/svc"
)

const (
	mentionExpandRetryMin    = 100 * time.Millisecond
	mentionExpandRetryMax    = 5 * time.Second
	mentionExpandMaxAttempts = 8
)

// mentionExpander consumes message created/updated events and materializes
// role and @everyone mention targets into message_mentions (source 2) so
// read-state mention counts include expanded mentions.
type mentionExpander struct {
	consumer *partitionconsumer.Consumer
	store    interface {
		GetMessage(ctx context.Context, messageID int64) (*model.Message, error)
		RebuildExpandedMessageMentions(ctx context.Context, messageID, expectedRevision int64, userIDs []int64) (bool, error)
	}
	guild guildv1.GuildServiceClient
}

// NewMentionExpander creates the expansion worker consumer, or returns a nil
// expander when Kafka is not configured.
func NewMentionExpander(svcCtx *svc.ServiceContext) (*mentionExpander, error) {
	if len(svcCtx.Cfg.Kafka.Seeds) == 0 {
		return nil, nil
	}
	expander := &mentionExpander{
		store: svcCtx.Store,
		guild: svcCtx.GuildClient,
	}
	consumer, err := partitionconsumer.New(
		partitionconsumer.Config{
			RetryMin:                mentionExpandRetryMin,
			RetryMax:                mentionExpandRetryMax,
			RetryMaxAttempts:        mentionExpandMaxAttempts,
			ShutdownTimeout:         svcCtx.Cfg.ShutdownDuration(),
			DropAfterRetryExhausted: true,
		},
		func(ctx context.Context, record *kgo.Record) (bool, error) {
			return true, expander.handleRecord(ctx, record)
		},
		kgo.SeedBrokers(svcCtx.Cfg.Kafka.Seeds...),
		kgo.ConsumerGroup(svcCtx.Cfg.Kafka.MentionsConsumerGroup),
		kgo.ConsumeTopics(svcCtx.Cfg.Kafka.EventTopic()),
	)
	if err != nil {
		return nil, err
	}
	expander.consumer = consumer
	return expander, nil
}

func (e *mentionExpander) Close() {
	_ = e.CloseContext(context.Background())
}

func (e *mentionExpander) CloseContext(ctx context.Context) error {
	if e != nil && e.consumer != nil {
		return e.consumer.CloseContext(ctx)
	}
	return nil
}

// Run polls the message event topic until ctx is cancelled, expanding each
// created/updated event that carries role or @everyone mentions.
func (e *mentionExpander) Run(ctx context.Context) {
	if e == nil || e.consumer == nil {
		return
	}
	e.consumer.Run(ctx)
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
	// Updated events only need expansion when content changed and mentions
	// were rebuilt; flags/attachment-only updates leave the expanded rows
	// untouched.
	if envelope.Type == EventTypeMessageUpdated && !payload.RebuildMentions {
		return nil
	}
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
	// The store re-checks the revision while holding the message row locked
	// and replaces the whole source-2 set, so a message edited during paging
	// cannot be left with stale expanded rows. A skipped rebuild (message
	// edited or deleted meanwhile) is not an error; the newer event owns it.
	_, err = e.store.RebuildExpandedMessageMentions(ctx, messageID, revision, targets)
	return err
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

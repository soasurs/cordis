package server

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/core/logx"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
	"github.com/soasurs/cordis/services/guild/v1/internal/svc"
)

const (
	profileProjectionBatch    = 100
	profileProjectRetryMin    = 100 * time.Millisecond
	profileProjectRetryMax    = 5 * time.Second
	profileProjectMaxAttempts = 8
)

type profileProjector struct {
	consumer *kgo.Client
	store    store.Store
	user     userv1.UserServiceClient
	rebuild  bool
}

// NewProfileProjector creates the Guild-local User profile projection worker.
// The worker also performs a bounded startup rebuild so rows created before
// this feature was deployed become searchable.
func NewProfileProjector(svcCtx *svc.ServiceContext) *profileProjector {
	return &profileProjector{
		consumer: svcCtx.ProfileConsumer,
		store:    svcCtx.Store,
		user:     svcCtx.UserClient,
		rebuild:  svcCtx.Cfg.Kafka.RebuildProfilesOnStart,
	}
}

func (p *profileProjector) Close() {
	if p != nil && p.consumer != nil {
		p.consumer.Close()
	}
}

func (p *profileProjector) Run(ctx context.Context) {
	if p == nil {
		return
	}
	if p.rebuild {
		p.rebuildProfilesUntilReady(ctx)
	}
	if p.consumer == nil {
		return
	}
	for {
		fetches := p.consumer.PollFetches(ctx)
		if ctx.Err() != nil {
			return
		}
		for _, fetchErr := range fetches.Errors() {
			logx.WithContext(ctx).Errorw("poll user profile event",
				logx.Field("topic", fetchErr.Topic),
				logx.Field("partition", fetchErr.Partition),
				logx.Field("error", fetchErr.Err),
			)
		}
		fetches.EachPartition(func(ftp kgo.FetchTopicPartition) {
			ftp.EachRecord(func(record *kgo.Record) {
				if err := p.handleRecord(ctx, record); err != nil {
					p.retryRecord(ctx, record)
					return
				}
				if err := p.consumer.CommitRecords(ctx, record); err != nil && ctx.Err() == nil {
					logx.WithContext(ctx).Errorw("commit user profile event", logx.Field("error", err))
				}
			})
		})
	}
}

func (p *profileProjector) rebuildProfilesUntilReady(ctx context.Context) {
	delay := profileProjectRetryMin
	for ctx.Err() == nil {
		if err := p.rebuildProfiles(ctx); err == nil {
			return
		} else {
			logx.WithContext(ctx).Errorw("rebuild guild member profile projection", logx.Field("error", err), logx.Field("retry_after", delay))
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, profileProjectRetryMax)
	}
}

func (p *profileProjector) retryRecord(ctx context.Context, record *kgo.Record) {
	delay := profileProjectRetryMin
	for attempt := 1; ctx.Err() == nil && attempt <= profileProjectMaxAttempts; attempt++ {
		logx.WithContext(ctx).Errorw("retry user profile event",
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
		if err := p.handleRecord(ctx, record); err == nil {
			if err := p.consumer.CommitRecords(ctx, record); err != nil && ctx.Err() == nil {
				logx.WithContext(ctx).Errorw("commit retried user profile event", logx.Field("error", err))
			}
			return
		}
		delay = min(delay*2, profileProjectRetryMax)
	}
	logx.WithContext(ctx).Errorw("drop user profile event after retries",
		logx.Field("topic", record.Topic),
		logx.Field("partition", record.Partition),
		logx.Field("offset", record.Offset),
	)
	if err := p.consumer.CommitRecords(ctx, record); err != nil && ctx.Err() == nil {
		logx.WithContext(ctx).Errorw("commit dropped user profile event", logx.Field("error", err))
	}
}

func (p *profileProjector) handleRecord(ctx context.Context, record *kgo.Record) error {
	var envelope eventEnvelope[userProfilePayload]
	if err := json.Unmarshal(record.Value, &envelope); err != nil {
		logx.WithContext(ctx).Errorw("drop malformed user profile event", logx.Field("error", err))
		return nil
	}
	if envelope.Type != realtime.EventUserProfileUpdated {
		return nil
	}
	userID, err := strconv.ParseInt(envelope.Data.UserID, 10, 64)
	if err != nil || userID <= 0 {
		return nil
	}
	avatarAssetID, err := strconv.ParseInt(envelope.Data.AvatarAssetID, 10, 64)
	if err != nil || avatarAssetID < 0 {
		return nil
	}
	return p.store.UpdateGuildMemberProfilesByUser(ctx, &model.GuildMemberProfile{
		UserID:           userID,
		Username:         envelope.Data.Username,
		Name:             envelope.Data.Name,
		AvatarAssetID:    avatarAssetID,
		ProfileUpdatedAt: max(envelope.Data.UpdatedAt, 0),
	})
}

func (p *profileProjector) rebuildProfiles(ctx context.Context) error {
	var afterGuildID, afterUserID int64
	for {
		keys, err := p.store.ListGuildMemberProfileKeys(ctx, store.ListGuildMemberProfileKeysParams{
			AfterGuildID: afterGuildID,
			AfterUserID:  afterUserID,
			Limit:        profileProjectionBatch,
		})
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			return nil
		}
		userIDs := make([]int64, 0, len(keys))
		seen := make(map[int64]struct{}, len(keys))
		for _, key := range keys {
			if _, ok := seen[key.UserID]; ok {
				continue
			}
			seen[key.UserID] = struct{}{}
			userIDs = append(userIDs, key.UserID)
		}
		request := new(userv1.BatchGetUserProfilesRequest)
		request.SetUserIds(userIDs)
		response, err := p.user.BatchGetUserProfiles(ctx, request)
		if err != nil {
			return err
		}
		if response == nil {
			return errors.New("user service returned an invalid profile response")
		}
		profiles := make(map[int64]*userv1.UserProfile, len(response.GetProfiles()))
		for _, profile := range response.GetProfiles() {
			if profile != nil && profile.GetUserId() > 0 {
				profiles[profile.GetUserId()] = profile
			}
		}
		for _, key := range keys {
			profile := profiles[key.UserID]
			if profile == nil {
				continue
			}
			if err := p.store.UpsertGuildMemberProfile(ctx, guildMemberProfileFromProto(key.GuildID, key.Nickname, profile)); err != nil {
				return err
			}
		}
		last := keys[len(keys)-1]
		afterGuildID, afterUserID = last.GuildID, last.UserID
		if len(keys) < profileProjectionBatch {
			return nil
		}
	}
}

func guildMemberProfileFromProto(guildID int64, nickname string, profile *userv1.UserProfile) *model.GuildMemberProfile {
	return &model.GuildMemberProfile{
		GuildID:          guildID,
		UserID:           profile.GetUserId(),
		Username:         profile.GetUsername(),
		Name:             profile.GetName(),
		Nickname:         nickname,
		AvatarAssetID:    profile.GetAvatarAssetId(),
		ProfileUpdatedAt: profile.GetUpdatedAt(),
	}
}

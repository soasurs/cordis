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
	"github.com/soasurs/cordis/pkg/kafka/partitionconsumer"
	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
	"github.com/soasurs/cordis/services/guild/v1/internal/svc"
)

const (
	profileProjectionBatch        = 100
	profileProjectRetryMin        = 100 * time.Millisecond
	profileProjectRetryMax        = 5 * time.Second
	profileProjectMaxAttempts     = 8
	profileProjectRebuildAttempts = 8
)

type profileProjector struct {
	consumer *partitionconsumer.Consumer
	store    store.Store
	user     userv1.UserServiceClient
	rebuild  bool
}

type userProfileUpdatedPayload struct {
	UserID        string  `json:"user_id"`
	Name          string  `json:"name"`
	AvatarAssetID *string `json:"avatar_asset_id"`
	UpdatedAt     int64   `json:"updated_at"`
	Username      string  `json:"username"`
}

// NewProfileProjector creates the Guild-local User profile projection worker.
// The worker also performs a bounded startup rebuild so rows created before
// this feature was deployed become searchable. If the rebuild cannot reach
// User after the retry budget is exhausted, event consumption still starts.
func NewProfileProjector(svcCtx *svc.ServiceContext) (*profileProjector, error) {
	projector := &profileProjector{
		store:   svcCtx.Store,
		user:    svcCtx.UserClient,
		rebuild: svcCtx.Cfg.Kafka.RebuildProfilesOnStart,
	}
	if len(svcCtx.Cfg.Kafka.Seeds) == 0 {
		return projector, nil
	}
	consumer, err := partitionconsumer.New(
		partitionconsumer.Config{
			RetryMin:                profileProjectRetryMin,
			RetryMax:                profileProjectRetryMax,
			RetryMaxAttempts:        profileProjectMaxAttempts,
			ShutdownTimeout:         svcCtx.Cfg.ShutdownDuration(),
			DropAfterRetryExhausted: true,
		},
		func(ctx context.Context, record *kgo.Record) (bool, error) {
			return true, projector.handleRecord(ctx, record)
		},
		kgo.SeedBrokers(svcCtx.Cfg.Kafka.Seeds...),
		kgo.ConsumerGroup(svcCtx.Cfg.Kafka.ProfileConsumerGroup),
		kgo.ConsumeTopics(svcCtx.Cfg.Kafka.UserTopic),
	)
	if err != nil {
		return nil, err
	}
	projector.consumer = consumer
	return projector, nil
}

func (p *profileProjector) Close() {
	_ = p.CloseContext(context.Background())
}

func (p *profileProjector) CloseContext(ctx context.Context) error {
	if p != nil && p.consumer != nil {
		return p.consumer.CloseContext(ctx)
	}
	return nil
}

func (p *profileProjector) Run(ctx context.Context) {
	if p == nil {
		return
	}
	if p.rebuild {
		p.rebuildProfilesAtStartup(ctx)
	}
	if p.consumer == nil {
		return
	}
	p.consumer.Run(ctx)
}

func (p *profileProjector) rebuildProfilesAtStartup(ctx context.Context) {
	p.rebuildProfilesWithRetry(ctx, profileProjectRebuildAttempts, profileProjectRetryMin, profileProjectRetryMax)
}

func (p *profileProjector) rebuildProfilesWithRetry(ctx context.Context, maxAttempts int, minDelay, maxDelay time.Duration) {
	delay := minDelay
	for attempt := 1; ctx.Err() == nil && attempt <= maxAttempts; attempt++ {
		err := p.rebuildProfiles(ctx)
		if err == nil {
			return
		} else if attempt == maxAttempts {
			logx.WithContext(ctx).Errorw("abandon guild member profile projection rebuild after retries",
				logx.Field("error", err), logx.Field("attempts", attempt))
			return
		}
		logx.WithContext(ctx).Errorw("rebuild guild member profile projection",
			logx.Field("error", err), logx.Field("attempt", attempt), logx.Field("retry_after", delay))
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, maxDelay)
	}
}

func (p *profileProjector) handleRecord(ctx context.Context, record *kgo.Record) error {
	var envelope eventEnvelope[userProfileUpdatedPayload]
	if err := json.Unmarshal(record.Value, &envelope); err != nil {
		logx.WithContext(ctx).Errorw("drop malformed user profile event", logx.Field("error", err))
		return nil
	}
	if envelope.Type != realtime.EventUserProfileUpdated {
		return nil
	}
	userID, err := strconv.ParseInt(envelope.Data.UserID, 10, 64)
	if err != nil || userID <= 0 {
		logx.WithContext(ctx).Errorw("drop user profile event with invalid user id",
			logx.Field("user_id", envelope.Data.UserID), logx.Field("error", err))
		return nil
	}
	profile := &model.GuildMemberProfile{
		UserID:           userID,
		Username:         envelope.Data.Username,
		Name:             envelope.Data.Name,
		ProfileUpdatedAt: max(envelope.Data.UpdatedAt, 0),
	}
	if envelope.Data.AvatarAssetID == nil {
		logx.WithContext(ctx).Infow("apply user profile event without avatar asset id",
			logx.Field("user_id", userID))
		return p.store.UpdateGuildMemberProfilesByUserWithoutAvatar(ctx, profile)
	}
	avatarAssetID, err := strconv.ParseInt(*envelope.Data.AvatarAssetID, 10, 64)
	if err != nil || avatarAssetID < 0 {
		logx.WithContext(ctx).Errorw("apply user profile event with invalid avatar asset id",
			logx.Field("user_id", userID), logx.Field("avatar_asset_id", *envelope.Data.AvatarAssetID), logx.Field("error", err))
		return p.store.UpdateGuildMemberProfilesByUserWithoutAvatar(ctx, profile)
	}
	profile.AvatarAssetID = avatarAssetID
	return p.store.UpdateGuildMemberProfilesByUser(ctx, profile)
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

func guildMemberProfilePlaceholder(guildID, userID int64, nickname string) *model.GuildMemberProfile {
	return &model.GuildMemberProfile{
		GuildID:  guildID,
		UserID:   userID,
		Nickname: nickname,
	}
}

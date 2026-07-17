package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/zeromicro/go-zero/core/logx"

	"github.com/soasurs/cordis/pkg/realtime"
	"github.com/soasurs/cordis/services/presence/v1/internal/store"
)

type eventEnvelope struct {
	Type string          `json:"t"`
	Data presencePayload `json:"d"`
}

type presencePayload struct {
	UserID    string   `json:"user_id"`
	Status    int32    `json:"status"`
	ChangedAt int64    `json:"changed_at"`
	GuildIDs  []string `json:"guild_ids"`
}

// previousStatus snapshots the aggregate before a mutation. The second
// return reports whether the snapshot succeeded; on failure the transition
// event is skipped rather than guessed.
func (s *presenceServer) previousStatus(ctx context.Context, userID int64) (store.PresenceStatus, bool) {
	presences, err := s.svcCtx.Store.ResolveUsersPresence(ctx, []int64{userID})
	if err != nil || len(presences) == 0 {
		if err != nil {
			logx.WithContext(ctx).Errorw("snapshot presence before mutation", logx.Field("error", err))
		}
		return store.PresenceStatusOffline, err == nil && len(presences) == 0
	}
	return presences[0].Status, true
}

// publishTransition emits presence.updated when the aggregate status
// actually changed. Heartbeat renewals with an unchanged aggregate stay
// silent, which keeps the stream to low-frequency transitions.
func (s *presenceServer) publishTransition(
	ctx context.Context,
	userID int64,
	guildIDs []int64,
	oldStatus store.PresenceStatus,
	known bool,
	newStatus store.PresenceStatus,
) {
	if s.svcCtx.Publisher == nil || !known || oldStatus == newStatus {
		return
	}
	changedAt := time.Now().UnixMilli()

	guilds := make([]string, 0, len(guildIDs))
	for _, guildID := range guildIDs {
		guilds = append(guilds, strconv.FormatInt(guildID, 10))
	}
	payload, err := json.Marshal(eventEnvelope{
		Type: realtime.EventPresenceUpdated,
		Data: presencePayload{
			UserID:    strconv.FormatInt(userID, 10),
			Status:    int32(newStatus),
			ChangedAt: changedAt,
			GuildIDs:  guilds,
		},
	})
	if err != nil {
		logx.WithContext(ctx).Errorw("build presence event", logx.Field("error", err))
		return
	}

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.svcCtx.Cfg.Kafka.PublishTimeout())
	defer cancel()
	key := fmt.Appendf(nil, "%d", userID)
	if err := s.svcCtx.Publisher.Publish(publishCtx, key, payload); err != nil {
		logx.WithContext(ctx).Errorw(
			"publish presence event",
			logx.Field("user_id", userID),
			logx.Field("error", err),
		)
	}
}

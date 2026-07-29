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
	Type           string          `json:"t"`
	Data           presencePayload `json:"d"`
	IdempotencyKey string          `json:"idempotency_key"`
}

type presencePayload struct {
	UserID    string   `json:"user_id"`
	Status    int32    `json:"status"`
	ChangedAt int64    `json:"changed_at"`
	Version   string   `json:"version"`
	GuildIDs  []string `json:"guild_ids"`
}

// publishTransition emits presence.updated when the aggregate status
// actually changed. Heartbeat renewals with an unchanged aggregate stay
// silent, which keeps the stream to low-frequency transitions.
func (s *presenceServer) publishTransition(
	ctx context.Context,
	presence store.UserPresence,
	guildIDs []int64,
) {
	if s.svcCtx.Publisher == nil {
		return
	}
	changedAt := time.Now().UnixMilli()

	guilds := make([]string, 0, len(guildIDs))
	for _, guildID := range guildIDs {
		guilds = append(guilds, strconv.FormatInt(guildID, 10))
	}
	payload, err := json.Marshal(eventEnvelope{
		Type:           realtime.EventPresenceUpdated,
		IdempotencyKey: strconv.FormatInt(presence.Version, 10),
		Data: presencePayload{
			UserID:    strconv.FormatInt(presence.UserID, 10),
			Status:    int32(presence.Status),
			ChangedAt: changedAt,
			Version:   strconv.FormatInt(presence.Version, 10),
			GuildIDs:  guilds,
		},
	})
	if err != nil {
		logx.WithContext(ctx).Errorw("build presence event", logx.Field("error", err))
		return
	}

	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.svcCtx.Cfg.Kafka.PublishTimeout())
	defer cancel()
	key := fmt.Appendf(nil, "%d", presence.UserID)
	if err := s.svcCtx.Publisher.Publish(publishCtx, key, payload); err != nil {
		logx.WithContext(ctx).Errorw(
			"publish presence event",
			logx.Field("user_id", presence.UserID),
			logx.Field("error", err),
		)
	}
}

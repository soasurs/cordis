package server

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/zeromicro/go-zero/core/logx"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
)

const guildListPageSize = 100

type guildEventEnvelope struct {
	Type string          `json:"t"`
	Data json.RawMessage `json:"d"`
}

type guildEventRouting struct {
	ID        string `json:"id"`
	GuildID   string `json:"guild_id"`
	ChannelID string `json:"channel_id"`
	UserID    string `json:"user_id"`
	OwnerID   string `json:"owner_id"`
}

func (c *client) loadGuildSubscriptions(ctx context.Context) error {
	var before int64
	for {
		req := new(guildv1.ListUserGuildsRequest)
		req.SetUserId(c.userID)
		req.SetBefore(before)
		req.SetLimit(guildListPageSize)
		resp, err := c.server.svcCtx.GuildClient.ListUserGuilds(ctx, req)
		if err != nil {
			return err
		}
		for _, guild := range resp.GetGuilds() {
			c.server.hub.subscribeGuild(c, guild.GetId())
		}
		if len(resp.GetGuilds()) < guildListPageSize || resp.GetBeforeCursor() == 0 {
			return nil
		}
		before = resp.GetBeforeCursor()
	}
}

func (s *Server) consumeGuildEvents(ctx context.Context) {
	defer s.guildEvents.Close()
	for {
		fetches := s.guildEvents.PollFetches(ctx)
		if ctx.Err() != nil {
			return
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			for _, fetchErr := range errs {
				logx.WithContext(ctx).Errorw("poll guild event",
					logx.Field("topic", fetchErr.Topic),
					logx.Field("partition", fetchErr.Partition),
					logx.Field("error", fetchErr.Err),
				)
			}
		}
		fetches.EachRecord(func(record *kgo.Record) {
			if err := s.handleGuildEvent(ctx, record.Value); err != nil {
				logx.WithContext(ctx).Errorw("handle guild event",
					logx.Field("topic", record.Topic),
					logx.Field("partition", record.Partition),
					logx.Field("offset", record.Offset),
					logx.Field("error", err),
				)
			}
			if err := s.guildEvents.CommitRecords(ctx, record); err != nil && ctx.Err() == nil {
				logx.WithContext(ctx).Errorw("commit guild event",
					logx.Field("topic", record.Topic),
					logx.Field("partition", record.Partition),
					logx.Field("offset", record.Offset),
					logx.Field("error", err),
				)
			}
		})
	}
}

func (s *Server) handleGuildEvent(ctx context.Context, value []byte) error {
	var event guildEventEnvelope
	if err := json.Unmarshal(value, &event); err != nil {
		return err
	}
	if event.Type == "" || !json.Valid(event.Data) {
		return errors.New("invalid guild event envelope")
	}
	var routing guildEventRouting
	if err := json.Unmarshal(event.Data, &routing); err != nil {
		return err
	}
	guildID, err := eventGuildID(event.Type, routing)
	if err != nil {
		return err
	}

	switch event.Type {
	case "guild.created":
		ownerID, _ := strconv.ParseInt(routing.OwnerID, 10, 64)
		s.subscribeLocalGuildUser(guildID, ownerID)
	case "guild.member.joined":
		userID, _ := strconv.ParseInt(routing.UserID, 10, 64)
		s.subscribeLocalGuildUser(guildID, userID)
	case "guild.channel.created", "guild.channel.updated",
		"guild.channel.overwrite.updated", "guild.channel.overwrite.deleted":
		channelID, err := eventChannelID(routing)
		if err != nil {
			return err
		}
		s.dispatchAuthorizedChannelEvent(ctx, guildID, channelID, event.Type, event.Data)
		return nil
	case "guild.channel.deleted":
		channelID, err := eventChannelID(routing)
		if err != nil {
			return err
		}
		dispatchClients(s.hub.channelClients(channelID), event.Type, event.Data)
		return nil
	}

	dispatchClients(s.hub.guildClients(guildID), event.Type, event.Data)
	switch event.Type {
	case "guild.member.removed", "guild.member.banned":
		userID, _ := strconv.ParseInt(routing.UserID, 10, 64)
		s.hub.unsubscribeGuildUser(guildID, userID)
	case "guild.deleted":
		s.hub.unsubscribeGuild(guildID)
	}
	return nil
}

func (s *Server) subscribeLocalGuildUser(guildID, userID int64) {
	for _, c := range s.hub.userClients(userID) {
		s.hub.subscribeGuild(c, guildID)
	}
}

func (s *Server) dispatchAuthorizedChannelEvent(ctx context.Context, guildID, channelID int64, eventType string, payload json.RawMessage) {
	for userID, clients := range s.hub.guildUserClients(guildID) {
		allowed, err := s.authorizeChannelSubscription(ctx, userID, channelID)
		if err != nil {
			logx.WithContext(ctx).Errorw("authorize guild channel event",
				logx.Field("guild_id", guildID),
				logx.Field("channel_id", channelID),
				logx.Field("user_id", userID),
				logx.Field("error", err),
			)
			continue
		}
		if allowed {
			dispatchClients(clients, eventType, payload)
		}
	}
}

func dispatchClients(clients []*client, eventType string, payload json.RawMessage) {
	for _, c := range clients {
		_ = c.dispatch(eventType, payload)
	}
}

func eventGuildID(eventType string, routing guildEventRouting) (int64, error) {
	value := routing.GuildID
	if value == "" && (eventType == "guild.created" || eventType == "guild.updated" || eventType == "guild.deleted") {
		value = routing.ID
	}
	guildID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || guildID <= 0 {
		return 0, errors.New("guild event guild id is invalid")
	}
	return guildID, nil
}

func eventChannelID(routing guildEventRouting) (int64, error) {
	value := routing.ChannelID
	if value == "" {
		value = routing.ID
	}
	channelID, err := strconv.ParseInt(value, 10, 64)
	if err != nil || channelID <= 0 {
		return 0, errors.New("guild event channel id is invalid")
	}
	return channelID, nil
}

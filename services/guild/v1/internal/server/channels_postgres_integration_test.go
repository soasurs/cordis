//go:build integration

package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	"github.com/soasurs/cordis/internal/testkit"
	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/migration"
	"github.com/soasurs/cordis/pkg/rpcerror"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/guild/v1/config"
	guildmigrations "github.com/soasurs/cordis/services/guild/v1/db/migrations"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
	"github.com/soasurs/cordis/services/guild/v1/internal/svc"
)

func TestConcurrentGuildChannelParentMovesPreservePositions(t *testing.T) {
	guildStore, service := newPostgresGuildService(t)

	const (
		guildID    = int64(19100)
		ownerID    = int64(29100)
		categoryID = int64(19101)
		firstID    = int64(19102)
		secondID   = int64(19103)
	)
	now := time.Now().UnixMilli()
	_, err := guildStore.CreateGuild(t.Context(), guildID, ownerID, "concurrent", now)
	require.NoError(t, err)
	_, err = guildStore.CreateGuildMember(t.Context(), guildID, ownerID, now)
	require.NoError(t, err)
	require.NoError(t, guildStore.CreateDefaultRole(t.Context(), guildID, now))
	_, err = guildStore.CreateGuildChannel(t.Context(), categoryID, guildID, "category", int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY), 0, "", 0, now)
	require.NoError(t, err)
	_, err = guildStore.CreateGuildChannel(t.Context(), firstID, guildID, "first", int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT), 1, "", 0, now)
	require.NoError(t, err)
	_, err = guildStore.CreateGuildChannel(t.Context(), secondID, guildID, "second", int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT), 2, "", 0, now)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, channelID := range []int64{firstID, secondID} {
		go func(channelID int64) {
			<-start
			req := new(guildv1.UpdateGuildChannelRequest)
			req.SetChannelId(channelID)
			req.SetActorUserId(ownerID)
			req.SetParentId(categoryID)
			req.SetExpectedChannelLayoutRevision(1)
			_, err := service.UpdateGuildChannel(ctx, req)
			results <- err
		}(channelID)
	}
	close(start)
	successes := 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
			continue
		}
		require.Equal(t, codes.Aborted, status.Code(err))
		require.True(t, rpcerror.Is(err, rpcerror.GuildDomain, rpcerror.GuildChannelLayoutConflict))
	}
	require.Equal(t, 1, successes)

	channels, err := guildStore.ListGuildChannels(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, channels, 3)
	require.Equal(t, categoryID, channels[0].ID)
	seen := make(map[int32]struct{}, len(channels))
	for position, channel := range channels {
		require.Equal(t, int32(position), channel.Position)
		if _, exists := seen[channel.Position]; exists {
			t.Fatalf("duplicate channel position %d", channel.Position)
		}
		seen[channel.Position] = struct{}{}
	}
	parented := 0
	for _, channel := range channels {
		if channel.ID != categoryID && channel.ParentID == categoryID {
			parented++
		}
	}
	require.Equal(t, 1, parented)
	layoutRevision, err := guildStore.GetGuildChannelLayoutRevision(ctx, guildID)
	require.NoError(t, err)
	require.Equal(t, int64(2), layoutRevision)
}

func TestConcurrentGuildChannelReordersPreservePositions(t *testing.T) {
	guildStore, service := newPostgresGuildService(t)

	const (
		guildID     = int64(19200)
		ownerID     = int64(29200)
		firstCatID  = int64(19201)
		secondCatID = int64(19202)
		firstID     = int64(19203)
		secondID    = int64(19204)
	)
	now := time.Now().UnixMilli()
	_, err := guildStore.CreateGuild(t.Context(), guildID, ownerID, "concurrent reorder", now)
	require.NoError(t, err)
	_, err = guildStore.CreateGuildMember(t.Context(), guildID, ownerID, now)
	require.NoError(t, err)
	require.NoError(t, guildStore.CreateDefaultRole(t.Context(), guildID, now))
	_, err = guildStore.CreateGuildChannel(t.Context(), firstCatID, guildID, "first category", int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY), 0, "", 0, now)
	require.NoError(t, err)
	_, err = guildStore.CreateGuildChannel(t.Context(), secondCatID, guildID, "second category", int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY), 1, "", 0, now)
	require.NoError(t, err)
	_, err = guildStore.CreateGuildChannel(t.Context(), firstID, guildID, "first", int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT), 2, "", 0, now)
	require.NoError(t, err)
	_, err = guildStore.CreateGuildChannel(t.Context(), secondID, guildID, "second", int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT), 3, "", 0, now)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		item := new(guildv1.GuildChannelPosition)
		item.SetChannelId(firstID)
		item.SetPosition(3)
		item.SetParentId(firstCatID)
		req := new(guildv1.ReorderGuildChannelsRequest)
		req.SetGuildId(guildID)
		req.SetActorUserId(ownerID)
		req.SetExpectedChannelLayoutRevision(1)
		req.SetPositions([]*guildv1.GuildChannelPosition{item})
		_, err := service.ReorderGuildChannels(ctx, req)
		results <- err
	}()
	go func() {
		<-start
		item := new(guildv1.GuildChannelPosition)
		item.SetChannelId(secondID)
		item.SetPosition(3)
		item.SetParentId(secondCatID)
		req := new(guildv1.ReorderGuildChannelsRequest)
		req.SetGuildId(guildID)
		req.SetActorUserId(ownerID)
		req.SetExpectedChannelLayoutRevision(1)
		req.SetPositions([]*guildv1.GuildChannelPosition{item})
		_, err := service.ReorderGuildChannels(ctx, req)
		results <- err
	}()
	close(start)
	successes := 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
			continue
		}
		require.Equal(t, codes.Aborted, status.Code(err))
		require.True(t, rpcerror.Is(err, rpcerror.GuildDomain, rpcerror.GuildChannelLayoutConflict))
	}
	require.Equal(t, 1, successes)

	channels, err := guildStore.ListGuildChannels(ctx, guildID)
	require.NoError(t, err)
	require.Len(t, channels, 4)
	seen := make(map[int32]struct{}, len(channels))
	byID := make(map[int64]*model.Channel, len(channels))
	for position, channel := range channels {
		require.Equal(t, int32(position), channel.Position)
		if _, exists := seen[channel.Position]; exists {
			t.Fatalf("duplicate channel position %d", channel.Position)
		}
		seen[channel.Position] = struct{}{}
		byID[channel.ID] = channel
	}
	require.NotNil(t, byID[firstID])
	require.NotNil(t, byID[secondID])
	parented := 0
	if byID[firstID].ParentID != 0 {
		parented++
	}
	if byID[secondID].ParentID != 0 {
		parented++
	}
	require.Equal(t, 1, parented)
	require.Contains(t, []int64{0, firstCatID}, byID[firstID].ParentID)
	require.Contains(t, []int64{0, secondCatID}, byID[secondID].ParentID)
	layoutRevision, err := guildStore.GetGuildChannelLayoutRevision(ctx, guildID)
	require.NoError(t, err)
	require.Equal(t, int64(2), layoutRevision)
}

func newPostgresGuildService(t *testing.T) (store.Store, guildv1.GuildServiceServer) {
	return newPostgresGuildServiceWithPublisher(t, nil)
}

func newPostgresGuildServiceWithPublisher(t *testing.T, publisher *fakePublisher) (store.Store, guildv1.GuildServiceServer) {
	t.Helper()
	postgres := testkit.StartPostgres(t)
	migrationDB, err := database.NewPostgres(database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, migrationDB.Close()) })
	db, err := database.NewPostgresPool(t.Context(), database.Config{DataSource: postgres.DSN})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	require.NoError(t, migration.Apply(t.Context(), migrationDB, guildmigrations.Files))

	node, err := snowflake.New()
	require.NoError(t, err)
	var guildStore store.Store = store.New(db)
	if publisher != nil {
		guildStore = &outboxObservingStore{Store: guildStore, observe: publisher.observe}
	}
	service := New(svc.NewServiceContextWithDependencies(config.Config{}, svc.Dependencies{
		Store:       guildStore,
		Snowflake:   node,
		Cursors:     testCursorCodec(t),
		UserClient:  &fakeUserClient{},
		MediaClient: &fakeMediaClient{},
	}))
	return guildStore, service
}

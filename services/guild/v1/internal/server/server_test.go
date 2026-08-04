package server

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/guild/v1/config"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
	"github.com/soasurs/cordis/services/guild/v1/internal/svc"
)

func TestCreateGuildCreatesOwnerDefaultRoleChannelsAndEvent(t *testing.T) {
	fakeStore := newFakeStore()
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName(" Cordis ")
	resp, err := server.CreateGuild(t.Context(), req)
	require.NoError(t, err)

	guild := resp.GetGuild()
	require.Equal(t, int64(1001), guild.GetOwnerId())
	require.Equal(t, "Cordis", guild.GetName())
	require.Equal(t, int64(1), guild.GetRevision())
	require.Contains(t, fakeStore.members[guild.GetId()], int64(1001))
	require.True(t, fakeStore.defaultRoles[guild.GetId()])

	channels, err := fakeStore.ListGuildChannels(t.Context(), guild.GetId())
	require.NoError(t, err)
	require.Len(t, channels, 4)
	require.Equal(t, defaultTextCategoryName, channels[0].Name)
	require.Equal(t, int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY), channels[0].Type)
	require.Equal(t, defaultTextChannelName, channels[1].Name)
	require.Equal(t, int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_TEXT), channels[1].Type)
	require.Equal(t, channels[0].ID, channels[1].ParentID)
	require.Equal(t, defaultVoiceCategoryName, channels[2].Name)
	require.Equal(t, int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_CATEGORY), channels[2].Type)
	require.Equal(t, defaultVoiceChannelName, channels[3].Name)
	require.Equal(t, int32(guildv1.GuildChannelType_GUILD_CHANNEL_TYPE_VOICE), channels[3].Type)
	require.Equal(t, channels[2].ID, channels[3].ParentID)
	require.Equal(t, "user_1001", fakeStore.profiles[guild.GetId()][1001].Username)

	for _, channel := range channels {
		overwrites, err := fakeStore.ListGuildChannelPermissionOverwrites(t.Context(), channel.ID)
		require.NoError(t, err)
		require.Len(t, overwrites, 1)
		require.Equal(t, int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_ROLE), overwrites[0].AppliesTo)
		require.Equal(t, guild.GetId(), overwrites[0].AppliesToID)
	}

	record := publisher.onlyRecord(t)
	require.Equal(t, string(record.key), guildIDString(guild.GetId()))
	var envelope eventEnvelope[guildPayload]
	require.NoError(t, json.Unmarshal(record.payload, &envelope))
	require.Equal(t, EventTypeGuildCreated, envelope.Type)
	require.Equal(t, guildIDString(guild.GetId()), envelope.Data.ID)
	require.Equal(t, "1001", envelope.Data.OwnerID)
	require.NotEmpty(t, envelope.IdempotencyKey)
}

func TestCreateGuildSurvivesProfileHydrationFailure(t *testing.T) {
	fakeStore := newFakeStore()
	userClient := &fakeUserClient{err: errors.New("user unavailable")}
	server := newTestGuildServerWithUser(t, fakeStore, nil, userClient)

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")
	resp, err := server.CreateGuild(t.Context(), req)
	require.NoError(t, err)

	profile := fakeStore.profiles[resp.GetGuild().GetId()][1001]
	require.NotNil(t, profile)
	require.Equal(t, int64(1001), profile.UserID)
	require.Empty(t, profile.Username)
}

func TestCreateGuildIdempotentReplayDoesNotRequireUserProfile(t *testing.T) {
	fakeStore := newFakeStore()
	userClient := &fakeUserClient{}
	server := newTestGuildServerWithUser(t, fakeStore, nil, userClient)

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")
	req.SetIdempotencyKey("guild-intent-1")
	first, err := server.CreateGuild(t.Context(), req)
	require.NoError(t, err)
	firstBatchCalls := userClient.batchCalls
	userClient.err = errors.New("user unavailable")

	replay, err := server.CreateGuild(t.Context(), req)
	require.NoError(t, err)
	require.Equal(t, first.GetGuild().GetId(), replay.GetGuild().GetId())
	require.Equal(t, firstBatchCalls, userClient.batchCalls)
}

func TestCreateGuildCommitFailureDoesNotPublish(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.transactErr = errors.New("commit failed")
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")
	_, err := server.CreateGuild(t.Context(), req)
	require.Error(t, err)
	require.Empty(t, publisher.records)
}

func TestPublishEventAddsCommittedAccessRevision(t *testing.T) {
	fakeStore := newFakeStore()
	guild := testGuild(10, 1001)
	guild.AccessRevision = 37
	fakeStore.guilds[10] = guild
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher).(*guildServer)
	event, err := newGuildRoleUpdatedEvent(&model.Role{ID: 20, GuildID: 10, Revision: 2}, 41)
	require.NoError(t, err)

	require.NoError(t, fakeStore.Transact(t.Context(), func(tx store.Store) error {
		return server.enqueueEvents(t.Context(), tx, []guildEvent{event})
	}))

	var envelope struct {
		IdempotencyKey string `json:"idempotency_key"`
		Data           struct {
			AccessRevision int64 `json:"access_revision"`
		} `json:"d"`
	}
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, "41", envelope.IdempotencyKey)
	require.Equal(t, int64(37), envelope.Data.AccessRevision)
}

func TestCreateGuildOutboxFailureFailsRequest(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.outboxErr = errors.New("outbox unavailable")
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)
	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")

	_, err := server.CreateGuild(t.Context(), req)
	require.Error(t, err)
	require.Empty(t, publisher.records)
	require.Empty(t, fakeStore.guildOutbox)
}

func TestCreateGuildMapsResourceLimit(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.quotaErr = store.ErrResourceLimitExceeded
	server := newTestGuildServer(t, fakeStore, nil)
	req := new(guildv1.CreateGuildRequest)
	req.SetOwnerId(1001)
	req.SetName("Cordis")

	_, err := server.CreateGuild(t.Context(), req)
	require.Equal(t, codes.ResourceExhausted, status.Code(err))
	require.Len(t, fakeStore.quotas, 1)
	require.Equal(t, store.QuotaOwnedGuilds, fakeStore.quotas[0].Kind)
}

func TestGetGuildHidesNonMember(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001)
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.GetGuildRequest)
	req.SetGuildId(10)
	req.SetUserId(1002)
	_, err := server.GetGuild(t.Context(), req)
	require.Equal(t, codes.NotFound, status.Code(err))
}

func TestUpdateGuildRequiresOwnerAndPreservesPresence(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002)
	fakeStore.profiles[10] = map[int64]*model.GuildMemberProfile{
		1001: {GuildID: 10, UserID: 1001, Username: "user_1001"},
		1002: {GuildID: 10, UserID: 1002, Username: "user_1002"},
	}
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	deniedReq := new(guildv1.UpdateGuildRequest)
	deniedReq.SetGuildId(10)
	deniedReq.SetActorUserId(1002)
	deniedReq.SetName("Renamed")
	_, err := server.UpdateGuild(t.Context(), deniedReq)
	require.Equal(t, codes.PermissionDenied, status.Code(err))

	updateReq := new(guildv1.UpdateGuildRequest)
	updateReq.SetGuildId(10)
	updateReq.SetActorUserId(1001)
	updateReq.SetDescription(" Community description ")
	resp, err := server.UpdateGuild(t.Context(), updateReq)
	require.NoError(t, err)
	require.Equal(t, "Guild", resp.GetGuild().GetName())
	require.Equal(t, "Community description", resp.GetGuild().GetDescription())
	require.Equal(t, int64(77), resp.GetGuild().GetIconAssetId())
	require.Equal(t, int64(2), resp.GetGuild().GetRevision())

	var envelope eventEnvelope[guildPayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeGuildUpdated, envelope.Type)
	require.Equal(t, "Community description", envelope.Data.Description)
	require.Equal(t, int64(2), envelope.Data.Revision)
}

func TestUpdateGuildCanClearDescription(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.guilds[10].Description = "old description"
	fakeStore.members[10] = testMembers(10, 1001)
	server := newTestGuildServer(t, fakeStore, nil)

	req := new(guildv1.UpdateGuildRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetDescription("")
	resp, err := server.UpdateGuild(t.Context(), req)
	require.NoError(t, err)
	require.Empty(t, resp.GetGuild().GetDescription())
}

func TestUpdateGuildRejectsLongDescription(t *testing.T) {
	server := newTestGuildServer(t, newFakeStore(), nil)
	req := new(guildv1.UpdateGuildRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetDescription(strings.Repeat("界", maxGuildDescriptionRunes+1))

	_, err := server.UpdateGuild(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestGuildIconUploadLifecycle(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001)
	mediaClient := &fakeMediaClient{asset: guildIconAsset(7001, 10, 1001)}
	publisher := new(fakePublisher)
	server := newTestGuildServerWithMedia(t, fakeStore, publisher, mediaClient)

	createReq := new(guildv1.CreateGuildIconUploadRequest)
	createReq.SetGuildId(10)
	createReq.SetActorUserId(1001)
	createReq.SetExpectedSize(123)
	createReq.SetContentType("image/png")
	createReq.SetIdempotencyKey("icon-intent-1")
	createResp, err := server.CreateGuildIconUpload(t.Context(), createReq)
	require.NoError(t, err)
	require.Equal(t, int64(7001), createResp.GetUploadId())
	require.Equal(t, map[string]string{"Content-Type": "image/png"}, createResp.GetRequestHeaders())
	require.Equal(t, mediav1.AssetStatus_ASSET_STATUS_CREATED, createResp.GetStatus())
	require.False(t, createResp.GetIdempotentReplay())
	require.Equal(t, int64(1001), mediaClient.createRequest.GetActorUserId())
	require.Equal(t, int64(10), mediaClient.createRequest.GetGuildIcon().GetGuildId())
	require.Equal(t, "icon-intent-1", mediaClient.createRequest.GetIdempotencyKey())

	completeReq := new(guildv1.CompleteGuildIconUploadRequest)
	completeReq.SetGuildId(10)
	completeReq.SetActorUserId(1001)
	completeReq.SetUploadId(7001)
	completeResp, err := server.CompleteGuildIconUpload(t.Context(), completeReq)
	require.NoError(t, err)
	require.Equal(t, int64(7001), completeResp.GetGuild().GetIconAssetId())
	require.Equal(t, int64(7001), mediaClient.completeRequest.GetUploadId())
	require.Len(t, publisher.records, 1)
}

func TestCompleteGuildIconUploadRejectsWrongSubject(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001)
	mediaClient := &fakeMediaClient{asset: guildIconAsset(7001, 20, 1001)}
	server := newTestGuildServerWithMedia(t, fakeStore, nil, mediaClient)

	req := new(guildv1.CompleteGuildIconUploadRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	req.SetUploadId(7001)
	_, err := server.CompleteGuildIconUpload(t.Context(), req)
	require.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Nil(t, mediaClient.completeRequest)
	require.Equal(t, int64(77), fakeStore.guilds[10].IconAssetID)
}

func TestDeleteGuildSoftDeletesChildrenAndPublishesEvent(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001, 1002)
	fakeStore.profiles[10] = map[int64]*model.GuildMemberProfile{
		1001: {GuildID: 10, UserID: 1001, Username: "user_1001"},
		1002: {GuildID: 10, UserID: 1002, Username: "user_1002"},
	}
	fakeStore.defaultRoles[10] = true
	publisher := new(fakePublisher)
	server := newTestGuildServer(t, fakeStore, publisher)

	req := new(guildv1.DeleteGuildRequest)
	req.SetGuildId(10)
	req.SetActorUserId(1001)
	resp, err := server.DeleteGuild(t.Context(), req)
	require.NoError(t, err)
	require.True(t, resp.GetOk())
	require.NotZero(t, fakeStore.guilds[10].DeletedAt)
	require.Empty(t, fakeStore.members[10])
	require.Empty(t, fakeStore.profiles[10])
	require.False(t, fakeStore.defaultRoles[10])

	var envelope eventEnvelope[guildDeletedPayload]
	require.NoError(t, json.Unmarshal(publisher.onlyRecord(t).payload, &envelope))
	require.Equal(t, EventTypeGuildDeleted, envelope.Type)
	require.Equal(t, "10", envelope.Data.ID)
	require.Equal(t, int64(2), envelope.Data.Revision)
	require.NotZero(t, envelope.Data.DeletedAt)
}

func TestListUserGuildsUsesDescendingCursor(t *testing.T) {
	fakeStore := newFakeStore()
	for _, id := range []int64{10, 20, 30} {
		fakeStore.guilds[id] = testGuild(id, 1001)
		fakeStore.members[id] = testMembers(id, 1001)
	}
	server := newTestGuildServer(t, fakeStore, nil)
	codec := testCursorCodec(t)
	token, err := codec.Encode(cursor.KindUserGuilds, userGuildsPayload{UserID: 1001, ID: 30})
	require.NoError(t, err)
	req := new(guildv1.ListUserGuildsRequest)
	req.SetUserId(1001)
	req.SetCursor(token)
	req.SetLimit(1)
	resp, err := server.ListUserGuilds(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, resp.GetGuilds(), 1)
	require.Equal(t, int64(20), resp.GetGuilds()[0].GetId())
	next, err := codec.Encode(cursor.KindUserGuilds, userGuildsPayload{UserID: 1001, ID: 20})
	require.NoError(t, err)
	require.Equal(t, next, resp.GetNextCursor())
}

func TestListUserGuildsPagesWithServerCursors(t *testing.T) {
	fakeStore := newFakeStore()
	for _, id := range []int64{10, 20, 30, 40} {
		fakeStore.guilds[id] = testGuild(id, 1001)
		fakeStore.members[id] = testMembers(id, 1001)
	}
	server := newTestGuildServer(t, fakeStore, nil)

	seen := make([]int64, 0, 4)
	req := new(guildv1.ListUserGuildsRequest)
	req.SetUserId(1001)
	req.SetLimit(1)
	for {
		resp, err := server.ListUserGuilds(t.Context(), req)
		require.NoError(t, err)
		require.Len(t, resp.GetGuilds(), 1)
		seen = append(seen, resp.GetGuilds()[0].GetId())
		if !resp.HasNextCursor() {
			break
		}
		req.SetCursor(resp.GetNextCursor())
	}
	require.Equal(t, []int64{40, 30, 20, 10}, seen)
}

func TestListUserGuildsRejectsBadCursors(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001)
	server := newTestGuildServer(t, fakeStore, nil)

	assertRejectsBadCursors(t, cursor.KindUserGuilds, userGuildsPayload{UserID: 1001, ID: 10}, func(token string) error {
		req := new(guildv1.ListUserGuildsRequest)
		req.SetUserId(1001)
		req.SetCursor(token)
		_, err := server.ListUserGuilds(t.Context(), req)
		return err
	})
}

func TestListUserGuildsRejectsCrossUserCursor(t *testing.T) {
	fakeStore := newFakeStore()
	fakeStore.guilds[10] = testGuild(10, 1001)
	fakeStore.members[10] = testMembers(10, 1001)
	server := newTestGuildServer(t, fakeStore, nil)

	codec := testCursorCodec(t)
	token, err := codec.Encode(cursor.KindUserGuilds, userGuildsPayload{UserID: 2002, ID: 10})
	require.NoError(t, err)
	req := new(guildv1.ListUserGuildsRequest)
	req.SetUserId(1001)
	req.SetCursor(token)
	_, err = server.ListUserGuilds(t.Context(), req)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func testCursorCodec(t *testing.T) *cursor.Codec {
	t.Helper()
	codec, err := cursor.NewCodec("test-cursor-secret-at-least-32-bytes!")
	require.NoError(t, err)
	return codec
}

// assertRejectsBadCursors checks empty, wrong-kind, and tampered tokens.

func assertRejectsBadCursors(t *testing.T, expectKind string, payload any, call func(token string) error) {
	t.Helper()
	require.Equal(t, codes.InvalidArgument, status.Code(call("")))

	codec := testCursorCodec(t)
	wrongKind := cursor.KindRelationships
	if expectKind == wrongKind {
		wrongKind = cursor.KindUserGuilds
	}
	wrong, err := codec.Encode(wrongKind, payload)
	require.NoError(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(call(wrong)))

	good, err := codec.Encode(expectKind, payload)
	require.NoError(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(call(good+"x")))
}

func newTestGuildServer(t *testing.T, fakeStore store.Store, publisher *fakePublisher) guildv1.GuildServiceServer {
	return newTestGuildServerWithMedia(t, fakeStore, publisher, &fakeMediaClient{})
}

func newTestGuildServerWithUser(
	t *testing.T,
	fakeStore store.Store,
	publisher *fakePublisher,
	userClient userv1.UserServiceClient,
) guildv1.GuildServiceServer {
	return newTestGuildServerWithUserAndMedia(t, fakeStore, publisher, userClient, &fakeMediaClient{})
}

func newTestGuildServerWithMedia(
	t *testing.T,
	fakeStore store.Store,
	publisher *fakePublisher,
	mediaClient mediav1.MediaServiceClient,
) guildv1.GuildServiceServer {
	return newTestGuildServerWithUserAndMedia(t, fakeStore, publisher, &fakeUserClient{}, mediaClient)
}

func newTestGuildServerWithUserAndMedia(
	t *testing.T,
	guildStore store.Store,
	publisher *fakePublisher,
	userClient userv1.UserServiceClient,
	mediaClient mediav1.MediaServiceClient,
) guildv1.GuildServiceServer {
	t.Helper()
	node, err := snowflake.New()
	require.NoError(t, err)
	if publisher != nil {
		if fakeS, ok := guildStore.(*fakeStore); ok {
			fakeS.outboxObserver = publisher.observe
		}
	}
	return New(&svc.ServiceContext{
		Cfg:   config.Config{},
		Store: guildStore, Snowflake: node, Cursors: testCursorCodec(t),
		UserClient:  userClient,
		MediaClient: mediaClient,
	})
}

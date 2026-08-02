package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/user/v1/config"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
	"github.com/soasurs/cordis/services/user/v1/internal/svc"
)

func newTestUserServer(t *testing.T, store store.Store) userv1.UserServiceServer {
	return newTestUserServerWithMedia(t, store, &fakeMediaClient{})
}

func newTestUserServerWithMedia(
	t *testing.T,
	store store.Store,
	mediaClient mediav1.MediaServiceClient,
) userv1.UserServiceServer {
	return newTestUserServerWithPublisher(t, store, mediaClient, nil)
}

func newTestUserServerWithPublisher(
	t *testing.T,
	store store.Store,
	mediaClient mediav1.MediaServiceClient,
	publisher svc.EventPublisher,
) userv1.UserServiceServer {
	t.Helper()

	node, err := snowflake.New()
	require.NoError(t, err)

	return New(&svc.ServiceContext{
		Cfg:         config.Config{Kafka: config.KafkaConfig{PublishTimeoutMs: 100}},
		Store:       store,
		Snowflake:   node,
		Cursors:     mustTestCursorCodec(t),
		MediaClient: mediaClient,
		Publisher:   publisher,
	})
}

func assertProfileUpdatedEvent(t *testing.T, publisher *fakeUserPublisher, profile *model.UserProfile) {
	t.Helper()
	require.Len(t, publisher.records, 1)
	require.Equal(t, "1001", publisher.records[0].key)
	var envelope eventEnvelope[userProfilePayload]
	require.NoError(t, json.Unmarshal(publisher.records[0].payload, &envelope))
	require.Equal(t, EventTypeUserProfileUpdated, envelope.Type)
	require.NotEmpty(t, envelope.IdempotencyKey)
	require.Equal(t, userProfilePayloadFromModel(profile), envelope.Data)
}

func mustTestCursorCodec(t *testing.T) *cursor.Codec {
	t.Helper()
	codec, err := cursor.NewCodec("test-cursor-secret-at-least-32-bytes!")
	require.NoError(t, err)
	return codec
}

func avatarAsset(assetID, userID int64) *mediav1.Asset {
	asset := new(mediav1.Asset)
	asset.SetId(assetID)
	asset.SetCreatedByUserId(userID)
	asset.SetSubjectId(userID)
	asset.SetKind(mediav1.AssetKind_ASSET_KIND_USER_AVATAR)
	asset.SetStatus(mediav1.AssetStatus_ASSET_STATUS_CREATED)
	return asset
}

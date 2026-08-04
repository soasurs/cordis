package server

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/pkg/kafka"
	"github.com/soasurs/cordis/pkg/outbox"
)

type fakeUserClient struct {
	userv1.UserServiceClient
	requestedUserID int64
	batchCalls      int
	err             error
}

func (f *fakeUserClient) GetUser(_ context.Context, req *userv1.GetUserRequest, _ ...grpc.CallOption) (*userv1.GetUserResponse, error) {
	f.requestedUserID = req.GetUserId()
	if f.err != nil {
		return nil, f.err
	}
	user := new(userv1.User)
	user.SetUserId(req.GetUserId())
	resp := new(userv1.GetUserResponse)
	resp.SetUser(user)
	return resp, nil
}

func (f *fakeUserClient) BatchGetUserProfiles(
	_ context.Context,
	req *userv1.BatchGetUserProfilesRequest,
	_ ...grpc.CallOption,
) (*userv1.BatchGetUserProfilesResponse, error) {
	f.batchCalls++
	if f.err != nil {
		return nil, f.err
	}
	profiles := make([]*userv1.UserProfile, 0, len(req.GetUserIds()))
	for _, userID := range req.GetUserIds() {
		profile := new(userv1.UserProfile)
		profile.SetUserId(userID)
		profile.SetUsername("user_" + strconv.FormatInt(userID, 10))
		profile.SetName("User " + strconv.FormatInt(userID, 10))
		profile.SetBio("Bio " + strconv.FormatInt(userID, 10))
		profile.SetAvatarAssetId(userID + 1000)
		profiles = append(profiles, profile)
	}
	resp := new(userv1.BatchGetUserProfilesResponse)
	resp.SetProfiles(profiles)
	return resp, nil
}

type fakeMediaClient struct {
	mediav1.MediaServiceClient
	asset           *mediav1.Asset
	createRequest   *mediav1.CreateUploadRequest
	completeRequest *mediav1.CompleteUploadRequest
	abortRequest    *mediav1.AbortUploadRequest
}

func (f *fakeMediaClient) CreateUpload(
	_ context.Context,
	req *mediav1.CreateUploadRequest,
	_ ...grpc.CallOption,
) (*mediav1.CreateUploadResponse, error) {
	f.createRequest = req
	resp := new(mediav1.CreateUploadResponse)
	resp.SetUploadId(7001)
	resp.SetPresignedUrl("https://upload.example/7001")
	resp.SetExpiresAt(9001)
	resp.SetRequestHeaders(map[string]string{"Content-Type": "image/png"})
	resp.SetStatus(mediav1.AssetStatus_ASSET_STATUS_CREATED)
	resp.SetIdempotentReplay(false)
	return resp, nil
}

func (f *fakeMediaClient) GetAsset(
	_ context.Context,
	_ *mediav1.GetAssetRequest,
	_ ...grpc.CallOption,
) (*mediav1.GetAssetResponse, error) {
	resp := new(mediav1.GetAssetResponse)
	resp.SetAsset(f.asset)
	return resp, nil
}

func (f *fakeMediaClient) CompleteUpload(
	_ context.Context,
	req *mediav1.CompleteUploadRequest,
	_ ...grpc.CallOption,
) (*mediav1.CompleteUploadResponse, error) {
	f.completeRequest = req
	resp := new(mediav1.CompleteUploadResponse)
	resp.SetAssetId(req.GetUploadId())
	return resp, nil
}

func (f *fakeMediaClient) AbortUpload(
	_ context.Context,
	req *mediav1.AbortUploadRequest,
	_ ...grpc.CallOption,
) (*mediav1.AbortUploadResponse, error) {
	f.abortRequest = req
	return new(mediav1.AbortUploadResponse), nil
}

func guildIconAsset(assetID, guildID, actorUserID int64) *mediav1.Asset {
	asset := new(mediav1.Asset)
	asset.SetId(assetID)
	asset.SetCreatedByUserId(actorUserID)
	asset.SetSubjectId(guildID)
	asset.SetKind(mediav1.AssetKind_ASSET_KIND_GUILD_ICON)
	asset.SetStatus(mediav1.AssetStatus_ASSET_STATUS_CREATED)
	return asset
}

type publishedRecord struct {
	key     []byte
	payload []byte
}

type fakePublisher struct {
	records    []publishedRecord
	err        error
	batchCalls int
}

func (p *fakePublisher) observe(records []outbox.Record) {
	p.batchCalls++
	for _, record := range records {
		p.records = append(p.records, publishedRecord{
			key: append([]byte(nil), record.Key...), payload: append([]byte(nil), record.Payload...),
		})
	}
}

func (p *fakePublisher) Publish(_ context.Context, key, payload []byte) error {
	p.records = append(p.records, publishedRecord{
		key: append([]byte(nil), key...), payload: append([]byte(nil), payload...),
	})
	return p.err
}

func (p *fakePublisher) PublishBatch(_ context.Context, records []kafka.Record) error {
	p.batchCalls++
	for _, record := range records {
		p.records = append(p.records, publishedRecord{
			key: append([]byte(nil), record.Key...), payload: append([]byte(nil), record.Payload...),
		})
	}
	return p.err
}

func (p *fakePublisher) onlyRecord(t *testing.T) publishedRecord {
	t.Helper()
	require.Len(t, p.records, 1)
	return p.records[0]
}

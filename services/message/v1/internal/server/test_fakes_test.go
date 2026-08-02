package server

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	mediav1 "github.com/soasurs/cordis/gen/media/v1"
	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

type fakeGuildClient struct {
	guildv1.GuildServiceClient
	mu                     sync.Mutex
	allowManageMessages    bool
	denyAll                bool
	permissions            uint64
	roles                  []*guildv1.GuildRole
	mentionTargets         []int64
	mentionTargetPages     [][]int64
	visibleMentionUserIDs  []int64
	visibleMentionRequests [][]int64
	channelType            guildv1.GuildChannelType
	authorizeRequests      []*guildv1.AuthorizeGuildChannelRequest
	visibleTextChannelIDs  []int64
}

type fakeMediaClient struct {
	mediav1.MediaServiceClient
	asset           *mediav1.Asset
	createRequest   *mediav1.CreateUploadRequest
	completeRequest *mediav1.CompleteUploadRequest
	abortRequest    *mediav1.AbortUploadRequest
	batchRequests   []*mediav1.BatchGetAssetURLsRequest
	batchResponse   *mediav1.BatchGetAssetURLsResponse
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
	resp.SetRequestHeaders(map[string]string{"Content-Type": req.GetContentType()})
	resp.SetStatus(mediav1.AssetStatus_ASSET_STATUS_CREATED)
	resp.SetIdempotentReplay(false)
	return resp, nil
}

func (f *fakeMediaClient) GetAsset(
	_ context.Context,
	req *mediav1.GetAssetRequest,
	_ ...grpc.CallOption,
) (*mediav1.GetAssetResponse, error) {
	asset := f.asset
	if asset == nil {
		asset = attachmentAsset(req.GetAssetId(), 10, 20)
		asset.SetStatus(mediav1.AssetStatus_ASSET_STATUS_READY)
		asset.SetSize(10)
		asset.SetContentType("image/png")
		asset.SetFilename("file.png")
		asset.SetWidth(1)
		asset.SetHeight(1)
		asset.SetBlurhash("LEHV6nWB2yk8pyo0adR*.7kCMdnj")
		asset.SetUrl("https://download.example/" + strconv.FormatInt(req.GetAssetId(), 10))
		asset.SetUrlExpiresAt(9001)
	}
	resp := new(mediav1.GetAssetResponse)
	resp.SetAsset(asset)
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
	metadata := new(mediav1.AssetMetadata)
	metadata.SetSize(123)
	metadata.SetContentType("application/pdf")
	metadata.SetFilename("report.pdf")
	metadata.SetUrl("https://download.example/" + strconv.FormatInt(req.GetUploadId(), 10))
	metadata.SetUrlExpiresAt(9001)
	resp.SetMetadata(metadata)
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

func (f *fakeMediaClient) BatchGetAssetURLs(
	_ context.Context,
	req *mediav1.BatchGetAssetURLsRequest,
	_ ...grpc.CallOption,
) (*mediav1.BatchGetAssetURLsResponse, error) {
	f.batchRequests = append(f.batchRequests, req)
	if f.batchResponse != nil {
		return f.batchResponse, nil
	}
	values := make([]*mediav1.AssetURL, 0, len(req.GetAssetIds()))
	for _, assetID := range req.GetAssetIds() {
		value := new(mediav1.AssetURL)
		value.SetAssetId(assetID)
		value.SetUrl("https://download.example/" + strconv.FormatInt(assetID, 10))
		value.SetExpiresAt(9001)
		values = append(values, value)
	}
	resp := new(mediav1.BatchGetAssetURLsResponse)
	resp.SetAssets(values)
	return resp, nil
}

func attachmentAsset(assetID, channelID, actorUserID int64) *mediav1.Asset {
	asset := new(mediav1.Asset)
	asset.SetId(assetID)
	asset.SetCreatedByUserId(actorUserID)
	asset.SetSubjectId(channelID)
	asset.SetKind(mediav1.AssetKind_ASSET_KIND_MESSAGE_ATTACHMENT)
	asset.SetStatus(mediav1.AssetStatus_ASSET_STATUS_CREATED)
	return asset
}

func (f *fakeGuildClient) GetUserGuildChannelVisibility(
	_ context.Context,
	req *guildv1.GetUserGuildChannelVisibilityRequest,
	_ ...grpc.CallOption,
) (*guildv1.GetUserGuildChannelVisibilityResponse, error) {
	visibility := new(guildv1.GuildChannelVisibility)
	visibility.SetGuildId(req.GetGuildId())
	visibility.SetAccessRevision(1)
	visibility.SetVisibleTextChannelIds(f.visibleTextChannelIDs)
	resp := new(guildv1.GetUserGuildChannelVisibilityResponse)
	resp.SetVisibility(visibility)
	return resp, nil
}

func (f *fakeGuildClient) AuthorizeGuildChannel(
	_ context.Context,
	req *guildv1.AuthorizeGuildChannelRequest,
	_ ...grpc.CallOption,
) (*guildv1.AuthorizeGuildChannelResponse, error) {
	f.mu.Lock()
	f.authorizeRequests = append(f.authorizeRequests, req)
	f.mu.Unlock()
	resp := new(guildv1.AuthorizeGuildChannelResponse)
	resp.SetAllowed(!f.denyAll && (req.GetPermission()&permissionManageMessages == 0 || f.allowManageMessages))
	resp.SetGuildId(9001)
	permissions := f.permissions
	if permissions == 0 {
		permissions = permissionViewChannel | permissionSendMessages
	}
	resp.SetPermissions(permissions)
	resp.SetChannelType(f.channelType)
	return resp, nil
}

func (f *fakeGuildClient) ListGuildRoles(
	_ context.Context,
	_ *guildv1.ListGuildRolesRequest,
	_ ...grpc.CallOption,
) (*guildv1.ListGuildRolesResponse, error) {
	resp := new(guildv1.ListGuildRolesResponse)
	resp.SetRoles(f.roles)
	return resp, nil
}

func (f *fakeGuildClient) FilterGuildChannelVisibleUsers(
	_ context.Context,
	req *guildv1.FilterGuildChannelVisibleUsersRequest,
	_ ...grpc.CallOption,
) (*guildv1.FilterGuildChannelVisibleUsersResponse, error) {
	f.visibleMentionRequests = append(f.visibleMentionRequests, append([]int64(nil), req.GetUserIds()...))
	visible := req.GetUserIds()
	if f.visibleMentionUserIDs != nil {
		allowed := make(map[int64]struct{}, len(f.visibleMentionUserIDs))
		for _, userID := range f.visibleMentionUserIDs {
			allowed[userID] = struct{}{}
		}
		visible = visible[:0]
		for _, userID := range req.GetUserIds() {
			if _, ok := allowed[userID]; ok {
				visible = append(visible, userID)
			}
		}
	}
	resp := new(guildv1.FilterGuildChannelVisibleUsersResponse)
	resp.SetUserIds(visible)
	return resp, nil
}

func (f *fakeGuildClient) ListGuildMentionTargets(
	_ context.Context,
	req *guildv1.ListGuildMentionTargetsRequest,
	_ ...grpc.CallOption,
) (*guildv1.ListGuildMentionTargetsResponse, error) {
	resp := new(guildv1.ListGuildMentionTargetsResponse)
	if len(f.mentionTargetPages) > 0 {
		index := 0
		if req.HasCursor() {
			index, _ = strconv.Atoi(req.GetCursor())
		}
		if index >= 0 && index < len(f.mentionTargetPages) {
			resp.SetUserIds(f.mentionTargetPages[index])
			if index+1 < len(f.mentionTargetPages) {
				resp.SetNextCursor(strconv.Itoa(index + 1))
			}
		}
		return resp, nil
	}
	resp.SetUserIds(f.mentionTargets)
	return resp, nil
}

func pbAttachment(assetID int64) *messagev1.Attachment {
	attachment := new(messagev1.Attachment)
	attachment.SetAssetId(assetID)
	attachment.SetFilename("file.png")
	attachment.SetSize(10)
	attachment.SetContentType("image/png")
	attachment.SetWidth(1)
	attachment.SetHeight(1)
	return attachment
}

type publishedRecord struct {
	key     []byte
	payload []byte
}

type fakePublisher struct {
	records []publishedRecord
	err     error
}

type fakeReadStatesLimiter struct {
	weights  []int64
	releases int
}

func (l *fakeReadStatesLimiter) Acquire(_ context.Context, weight int64) (func(), error) {
	l.weights = append(l.weights, weight)
	return func() { l.releases++ }, nil
}

func (p *fakePublisher) Publish(_ context.Context, key, payload []byte) error {
	p.records = append(p.records, publishedRecord{
		key:     append([]byte(nil), key...),
		payload: append([]byte(nil), payload...),
	})
	return p.err
}

func (p *fakePublisher) onlyRecord(t *testing.T) publishedRecord {
	t.Helper()
	require.Len(t, p.records, 1)
	return p.records[0]
}

type fakeStore struct {
	messages        map[int64]*model.Message
	mentions        map[int64]model.MessageMentions
	dmChannels      map[int64]*model.DmChannel
	readStates      map[int64]map[int64]int64 // userID -> channelID -> lastReadID
	idempotency     map[string]fakeIdempotencyRecord
	listReadyCalls  int
	readyBatchSizes []int
	rebuildBatches  [][]int64
	rebuildStale    bool
	transactErr     error
	getMessageErr   error
}

type fakeIdempotencyRecord struct {
	messageID   int64
	requestHash []byte
	expiresAt   int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		messages:    make(map[int64]*model.Message),
		mentions:    make(map[int64]model.MessageMentions),
		dmChannels:  make(map[int64]*model.DmChannel),
		readStates:  make(map[int64]map[int64]int64),
		idempotency: make(map[string]fakeIdempotencyRecord),
	}
}

func (s *fakeStore) Transact(_ context.Context, fn func(txStore store.Store) error) error {
	if err := fn(s); err != nil {
		return err
	}
	return s.transactErr
}

func (s *fakeStore) ClaimMessageIdempotency(
	_ context.Context,
	params store.ClaimMessageIdempotencyParams,
) (*store.MessageIdempotencyClaim, error) {
	key := strconv.FormatInt(params.ActorUserID, 10) + "\x1f" + params.Operation + "\x1f" + params.IdempotencyKey
	if existing, ok := s.idempotency[key]; ok {
		if existing.expiresAt <= params.CreatedAt {
			delete(s.idempotency, key)
		} else {
			return &store.MessageIdempotencyClaim{
				MessageID:   existing.messageID,
				RequestHash: append([]byte(nil), existing.requestHash...),
			}, nil
		}
	}
	s.idempotency[key] = fakeIdempotencyRecord{
		messageID:   params.MessageID,
		requestHash: append([]byte(nil), params.RequestHash...),
		expiresAt:   params.ExpiresAt,
	}
	return &store.MessageIdempotencyClaim{
		MessageID:   params.MessageID,
		RequestHash: append([]byte(nil), params.RequestHash...),
		Claimed:     true,
	}, nil
}

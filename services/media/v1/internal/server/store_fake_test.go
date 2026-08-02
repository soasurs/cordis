package server

import (
	"context"
	"strconv"
	"sync"

	"github.com/soasurs/cordis/services/media/v1/internal/store"
)

type fakeStore struct {
	mu          sync.Mutex
	assets      map[int64]*store.Asset
	locks       map[int64]*sync.Mutex
	idempotency map[string]fakeMediaIdempotencyRecord
}

type fakeMediaIdempotencyRecord struct {
	assetID     int64
	requestHash []byte
	expiresAt   int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		assets:      make(map[int64]*store.Asset),
		locks:       make(map[int64]*sync.Mutex),
		idempotency: make(map[string]fakeMediaIdempotencyRecord),
	}
}

func (f *fakeStore) Transact(_ context.Context, fn func(txStore store.Store) error) error {
	return fn(f)
}

func (f *fakeStore) ClaimMediaIdempotency(
	_ context.Context,
	params store.ClaimMediaIdempotencyParams,
) (*store.MediaIdempotencyClaim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strconv.FormatInt(params.ActorUserID, 10) + "\x1f" + params.Operation + "\x1f" + params.IdempotencyKey
	if existing, ok := f.idempotency[key]; ok {
		if existing.expiresAt <= params.CreatedAt {
			delete(f.idempotency, key)
		} else {
			return &store.MediaIdempotencyClaim{
				AssetID:     existing.assetID,
				RequestHash: append([]byte(nil), existing.requestHash...),
			}, nil
		}
	}
	f.idempotency[key] = fakeMediaIdempotencyRecord{
		assetID:     params.AssetID,
		requestHash: append([]byte(nil), params.RequestHash...),
		expiresAt:   params.ExpiresAt,
	}
	return &store.MediaIdempotencyClaim{
		AssetID:     params.AssetID,
		RequestHash: append([]byte(nil), params.RequestHash...),
		Claimed:     true,
	}, nil
}

func (f *fakeStore) CreateAssetWithQuota(
	_ context.Context,
	asset *store.Asset,
	activeUploadLimit int64,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var count int64
	for _, current := range f.assets {
		if current.CreatedByUserID == asset.CreatedByUserID &&
			current.Status == store.StatusCreated {
			count++
		}
	}
	if count >= activeUploadLimit {
		return store.ErrActiveUploadLimit
	}
	f.assets[asset.ID] = asset
	return nil
}

func (f *fakeStore) createAsset(asset *store.Asset) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.assets[asset.ID] = asset
}

func (f *fakeStore) GetAsset(_ context.Context, id int64) (*store.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	asset, ok := f.assets[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return asset, nil
}

func (f *fakeStore) ListAssets(_ context.Context, ids []int64) ([]*store.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	assets := make([]*store.Asset, 0, len(ids))
	for _, id := range ids {
		if asset := f.assets[id]; asset != nil {
			assets = append(assets, asset)
		}
	}
	return assets, nil
}

func (f *fakeStore) UpdateAsset(_ context.Context, asset *store.Asset) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.assets[asset.ID]; !ok {
		return store.ErrNotFound
	}
	f.assets[asset.ID] = asset
	return nil
}

func (f *fakeStore) ListExpiredUploads(_ context.Context, before int64) ([]*store.Asset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var expired []*store.Asset
	for _, asset := range f.assets {
		if asset.ExpiresAt > 0 &&
			asset.ExpiresAt <= before &&
			asset.Status == store.StatusCreated {
			expired = append(expired, asset)
		}
	}
	return expired, nil
}

func (f *fakeStore) AcquireAssetLock(
	_ context.Context,
	id int64,
) (store.AssetStore, func(), error) {
	f.mu.Lock()
	lock := f.locks[id]
	if lock == nil {
		lock = new(sync.Mutex)
		f.locks[id] = lock
	}
	f.mu.Unlock()
	lock.Lock()
	return f, lock.Unlock, nil
}

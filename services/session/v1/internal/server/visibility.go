package server

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
)

type visibilitySnapshot struct {
	accessRevision int64
	channelIDs     []int64
}

func (s *visibilitySnapshot) contains(channelID int64) bool {
	_, found := slices.BinarySearch(s.channelIDs, channelID)
	return found
}

type userVisibilityState struct {
	references int
	snapshots  map[int64]*visibilityEntry
}

type visibilityEntry struct {
	snapshot         *visibilitySnapshot
	requiredRevision int64
	generation       uint64
	reconcileSent    bool
}

func (s *Server) loadOrReuseVisibilitySnapshots(ctx context.Context, userID int64) (map[int64]*visibilitySnapshot, error) {
	result := s.visibilityLoads.DoChan(strconv.FormatInt(userID, 10), func() (any, error) {
		if snapshots, ok := s.cachedVisibilitySnapshots(userID); ok {
			return snapshots, nil
		}
		return s.loadVisibilitySnapshots(ctx, userID)
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case loaded := <-result:
		if loaded.Err != nil {
			return nil, loaded.Err
		}
		return loaded.Val.(map[int64]*visibilitySnapshot), nil
	}
}

func (s *Server) loadVisibilitySnapshots(ctx context.Context, userID int64) (map[int64]*visibilitySnapshot, error) {
	snapshots := make(map[int64]*visibilitySnapshot)
	var before, previousGuildID int64
	for {
		req := new(guildv1.ListUserGuildChannelVisibilitiesRequest)
		req.SetUserId(userID)
		req.SetBeforeGuildId(before)
		req.SetLimit(guildPageSize)
		resp, err := s.svcCtx.GuildClient.ListUserGuildChannelVisibilities(ctx, req)
		if err != nil {
			return nil, err
		}
		visibilities := resp.GetVisibilities()
		if len(visibilities) > guildPageSize {
			return nil, status.Error(codes.Internal, "guild visibility page exceeds requested limit")
		}
		if len(snapshots)+len(visibilities) > s.svcCtx.Cfg.Node.VisibilityGuildLimit() {
			return nil, status.Error(codes.ResourceExhausted, "guild visibility limit exceeded")
		}
		for _, visibility := range visibilities {
			guildID := visibility.GetGuildId()
			if guildID <= 0 || visibility.GetAccessRevision() <= 0 ||
				(previousGuildID > 0 && guildID >= previousGuildID) {
				return nil, status.Error(codes.Internal, "guild visibility page is invalid")
			}
			channelIDs := visibility.GetVisibleChannelIds()
			if len(channelIDs) > s.svcCtx.Cfg.Node.VisibilityChannelLimit() || !strictlyIncreasingPositive(channelIDs) {
				return nil, status.Error(codes.Internal, "guild visibility channel ids are invalid")
			}
			snapshots[guildID] = &visibilitySnapshot{
				accessRevision: visibility.GetAccessRevision(),
				channelIDs:     slices.Clone(channelIDs),
			}
			previousGuildID = guildID
		}
		if len(visibilities) == 0 || len(visibilities) < guildPageSize {
			return snapshots, nil
		}
		cursor := resp.GetBeforeGuildId()
		if cursor <= 0 || cursor != previousGuildID || (before > 0 && cursor >= before) {
			return nil, status.Error(codes.Internal, "guild visibility cursor is invalid")
		}
		before = cursor
	}
}

// loadSingleVisibilitySnapshot loads channel visibility for one Guild through
// the dedicated single-guild RPC. It does not paginate or load other Guilds.
func (s *Server) loadSingleVisibilitySnapshot(ctx context.Context, userID, guildID int64) (*visibilitySnapshot, error) {
	req := new(guildv1.GetUserGuildChannelVisibilityRequest)
	req.SetUserId(userID)
	req.SetGuildId(guildID)
	resp, err := s.svcCtx.GuildClient.GetUserGuildChannelVisibility(ctx, req)
	if err != nil {
		return nil, err
	}
	v := resp.GetVisibility()
	if v == nil || v.GetGuildId() != guildID || v.GetAccessRevision() <= 0 {
		return nil, status.Error(codes.Internal, "guild visibility response is invalid")
	}
	channelIDs := v.GetVisibleChannelIds()
	if len(channelIDs) > s.svcCtx.Cfg.Node.VisibilityChannelLimit() || !strictlyIncreasingPositive(channelIDs) {
		return nil, status.Error(codes.Internal, "guild visibility channel ids are invalid")
	}
	return &visibilitySnapshot{
		accessRevision: v.GetAccessRevision(),
		channelIDs:     slices.Clone(channelIDs),
	}, nil
}

func strictlyIncreasingPositive(ids []int64) bool {
	for i, id := range ids {
		if id <= 0 || (i > 0 && id <= ids[i-1]) {
			return false
		}
	}
	return true
}

func (s *Server) cachedVisibilitySnapshots(userID int64) (map[int64]*visibilitySnapshot, bool) {
	s.visibilityMu.RLock()
	defer s.visibilityMu.RUnlock()
	state := s.visibilityUsers[userID]
	if state == nil || state.references == 0 {
		return nil, false
	}
	snapshots := make(map[int64]*visibilitySnapshot, len(state.snapshots))
	for guildID, entry := range state.snapshots {
		if entry == nil || entry.snapshot == nil {
			return nil, false
		}
		snapshots[guildID] = entry.snapshot
	}
	return snapshots, true
}

func (s *Server) retainVisibilitySnapshots(userID int64, snapshots map[int64]*visibilitySnapshot) {
	s.visibilityMu.Lock()
	defer s.visibilityMu.Unlock()
	state := s.visibilityUsers[userID]
	if state == nil {
		state = &userVisibilityState{snapshots: make(map[int64]*visibilityEntry, len(snapshots))}
		s.visibilityUsers[userID] = state
	}
	for guildID, snapshot := range snapshots {
		entry := state.snapshots[guildID]
		if entry == nil {
			state.snapshots[guildID] = &visibilityEntry{snapshot: snapshot}
			continue
		}
		if snapshot.accessRevision >= entry.requiredRevision &&
			(entry.snapshot == nil || snapshot.accessRevision > entry.snapshot.accessRevision) {
			entry.snapshot = snapshot
			entry.reconcileSent = false
		}
	}
	state.references++
}

func (s *Server) releaseVisibilitySnapshots(userID int64) {
	s.visibilityMu.Lock()
	defer s.visibilityMu.Unlock()
	state := s.visibilityUsers[userID]
	if state == nil {
		return
	}
	if state.references <= 1 {
		delete(s.visibilityUsers, userID)
		return
	}
	state.references--
}

func (s *Server) invalidateVisibilityGuild(userID, guildID, accessRevision int64) bool {
	s.visibilityMu.Lock()
	defer s.visibilityMu.Unlock()
	state := s.visibilityUsers[userID]
	if state == nil {
		return false
	}
	entry := state.snapshots[guildID]
	if entry == nil {
		entry = new(visibilityEntry)
		state.snapshots[guildID] = entry
	}
	if accessRevision > 0 {
		if entry.snapshot != nil && entry.snapshot.accessRevision >= accessRevision {
			return false
		}
		if accessRevision <= entry.requiredRevision && entry.snapshot == nil {
			return false
		}
		entry.requiredRevision = max(entry.requiredRevision, accessRevision)
	}
	entry.generation++
	entry.snapshot = nil
	entry.reconcileSent = false
	return true
}

func (s *Server) removeVisibilityGuild(userID, guildID int64) {
	s.visibilityMu.Lock()
	defer s.visibilityMu.Unlock()
	if state := s.visibilityUsers[userID]; state != nil {
		delete(state.snapshots, guildID)
	}
}

func (s *Server) visibilitySnapshotFor(userID, guildID int64) (*visibilitySnapshot, bool) {
	s.visibilityMu.RLock()
	defer s.visibilityMu.RUnlock()
	state := s.visibilityUsers[userID]
	if state == nil {
		return nil, false
	}
	entry := state.snapshots[guildID]
	if entry == nil || entry.snapshot == nil {
		return nil, false
	}
	return entry.snapshot, true
}

func (s *Server) ensureVisibilitySnapshot(ctx context.Context, userID, guildID int64) (*visibilitySnapshot, error) {
	if snapshot, ok := s.visibilitySnapshotFor(userID, guildID); ok {
		return snapshot, nil
	}
	reloadCtx, cancel := context.WithTimeout(ctx, s.svcCtx.Cfg.Node.SnapshotReloadTimeout())
	defer cancel()
	key := fmt.Sprintf("%d:%d", userID, guildID)
	result := s.visibilityReloads.DoChan(key, func() (any, error) {
		if err := s.visibilityReloadSem.Acquire(reloadCtx, 1); err != nil {
			return nil, err
		}
		defer s.visibilityReloadSem.Release(1)
		return s.reloadVisibilitySnapshot(reloadCtx, userID, guildID)
	})
	select {
	case <-reloadCtx.Done():
		return nil, reloadCtx.Err()
	case loaded := <-result:
		if loaded.Err != nil {
			return nil, loaded.Err
		}
		return loaded.Val.(*visibilitySnapshot), nil
	}
}

func (s *Server) reloadVisibilitySnapshot(ctx context.Context, userID, guildID int64) (*visibilitySnapshot, error) {
	for range 2 {
		generation, requiredRevision, ok := s.visibilityReloadTarget(userID, guildID)
		if !ok {
			return nil, status.Error(codes.FailedPrecondition, "guild visibility snapshot is not retained")
		}
		candidate, err := s.loadSingleVisibilitySnapshot(ctx, userID, guildID)
		if err != nil {
			return nil, err
		}
		if candidate.accessRevision < requiredRevision {
			return nil, status.Error(codes.Aborted, "guild visibility revision is stale")
		}
		if s.installReloadedVisibilitySnapshot(userID, guildID, generation, candidate) {
			return candidate, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, ctx.Err()
		}
	}
	return nil, status.Error(codes.Aborted, "guild visibility changed during reload")
}

func (s *Server) visibilityReloadTarget(userID, guildID int64) (uint64, int64, bool) {
	s.visibilityMu.RLock()
	defer s.visibilityMu.RUnlock()
	state := s.visibilityUsers[userID]
	if state == nil || state.references == 0 {
		return 0, 0, false
	}
	entry := state.snapshots[guildID]
	if entry == nil {
		return 0, 0, false
	}
	return entry.generation, entry.requiredRevision, true
}

func (s *Server) installReloadedVisibilitySnapshot(
	userID, guildID int64,
	generation uint64,
	snapshot *visibilitySnapshot,
) bool {
	s.visibilityMu.Lock()
	defer s.visibilityMu.Unlock()
	state := s.visibilityUsers[userID]
	if state == nil || state.references == 0 {
		return false
	}
	entry := state.snapshots[guildID]
	if entry == nil || entry.generation != generation || snapshot.accessRevision < entry.requiredRevision {
		return false
	}
	entry.snapshot = snapshot
	entry.reconcileSent = false
	return true
}

func (s *Server) markVisibilityReconcile(userID, guildID int64) bool {
	s.visibilityMu.Lock()
	defer s.visibilityMu.Unlock()
	state := s.visibilityUsers[userID]
	if state == nil {
		return false
	}
	entry := state.snapshots[guildID]
	if entry == nil || entry.snapshot != nil || entry.reconcileSent {
		return false
	}
	entry.reconcileSent = true
	return true
}

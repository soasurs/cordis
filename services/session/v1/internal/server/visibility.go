package server

import (
	"context"
	"fmt"
	"slices"

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

func (s *Server) loadReadyGuilds(
	ctx context.Context,
	userID int64,
) ([]*guildv1.ReadyGuild, map[int64]*visibilitySnapshot, error) {
	req := new(guildv1.GetUserReadyStateRequest)
	req.SetUserId(userID)
	resp, err := s.svcCtx.GuildClient.GetUserReadyState(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	readyGuilds := resp.GetGuilds()
	if len(readyGuilds) > s.svcCtx.Cfg.Node.VisibilityGuildLimit() {
		return nil, nil, status.Error(codes.ResourceExhausted, "guild visibility limit exceeded")
	}
	snapshots := make(map[int64]*visibilitySnapshot, len(readyGuilds))
	for _, ready := range readyGuilds {
		guildID := ready.GetGuild().GetId()
		if guildID <= 0 || ready.GetAccessRevision() <= 0 {
			return nil, nil, status.Error(codes.Internal, "guild ready state is invalid")
		}
		channelIDs := make([]int64, 0, len(ready.GetChannels()))
		for _, channel := range ready.GetChannels() {
			if channel.GetId() <= 0 || channel.GetGuildId() != guildID {
				return nil, nil, status.Error(codes.Internal, "guild ready channel is invalid")
			}
			channelIDs = append(channelIDs, channel.GetId())
		}
		if len(channelIDs) > s.svcCtx.Cfg.Node.VisibilityChannelLimit() {
			return nil, nil, status.Error(codes.ResourceExhausted, "guild visibility channel limit exceeded")
		}
		slices.Sort(channelIDs)
		if !strictlyIncreasingPositive(channelIDs) {
			return nil, nil, status.Error(codes.Internal, "guild ready channel ids are invalid")
		}
		if _, exists := snapshots[guildID]; exists {
			return nil, nil, status.Error(codes.Internal, "guild ready state contains duplicate guild")
		}
		snapshots[guildID] = &visibilitySnapshot{
			accessRevision: ready.GetAccessRevision(),
			channelIDs:     channelIDs,
		}
	}
	return readyGuilds, snapshots, nil
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

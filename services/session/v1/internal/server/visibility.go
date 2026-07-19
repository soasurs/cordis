package server

import (
	"context"
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
	snapshots  map[int64]*visibilitySnapshot
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
	for guildID, snapshot := range state.snapshots {
		if snapshot == nil {
			return nil, false
		}
		snapshots[guildID] = snapshot
	}
	return snapshots, true
}

func (s *Server) retainVisibilitySnapshots(userID int64, snapshots map[int64]*visibilitySnapshot) {
	s.visibilityMu.Lock()
	defer s.visibilityMu.Unlock()
	state := s.visibilityUsers[userID]
	if state == nil {
		state = &userVisibilityState{snapshots: make(map[int64]*visibilitySnapshot, len(snapshots))}
		s.visibilityUsers[userID] = state
	}
	for guildID, snapshot := range snapshots {
		current := state.snapshots[guildID]
		if current == nil || snapshot.accessRevision > current.accessRevision {
			state.snapshots[guildID] = snapshot
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

func (s *Server) invalidateVisibilityGuild(userID, guildID int64) {
	s.visibilityMu.Lock()
	defer s.visibilityMu.Unlock()
	if state := s.visibilityUsers[userID]; state != nil {
		state.snapshots[guildID] = nil
	}
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
	if state == nil || state.snapshots[guildID] == nil {
		return nil, false
	}
	return state.snapshots[guildID], true
}

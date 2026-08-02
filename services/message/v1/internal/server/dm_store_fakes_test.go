package server

import (
	"context"
	"database/sql"
	"slices"
	"sort"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

func (s *fakeStore) CreateDmChannel(_ context.Context, channel *model.DmChannel) error {
	for _, existing := range s.dmChannels {
		if existing.UserLo == channel.UserLo && existing.UserHi == channel.UserHi {
			return sql.ErrNoRows
		}
	}
	value := *channel
	s.dmChannels[channel.ID] = &value
	return nil
}

func (s *fakeStore) GetDmChannel(_ context.Context, channelID int64) (*model.DmChannel, error) {
	channel, ok := s.dmChannels[channelID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	value := *channel
	return &value, nil
}

func (s *fakeStore) GetDmChannelByPair(_ context.Context, userLo, userHi int64) (*model.DmChannel, error) {
	for _, channel := range s.dmChannels {
		if channel.UserLo == userLo && channel.UserHi == userHi {
			value := *channel
			return &value, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (s *fakeStore) ListDmChannels(_ context.Context, params store.ListDmChannelsParams) ([]*model.DmChannel, error) {
	var channels []*model.DmChannel
	for _, channel := range s.dmChannels {
		if channel.UserLo != params.UserID && channel.UserHi != params.UserID {
			continue
		}
		if params.BeforeID != 0 && channel.ID >= params.BeforeID {
			continue
		}
		value := *channel
		channels = append(channels, &value)
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i].ID > channels[j].ID })
	if len(channels) > params.Limit {
		channels = channels[:params.Limit]
	}
	return channels, nil
}

func (s *fakeStore) ListAllDmChannels(_ context.Context, userID int64) ([]*model.DmChannel, error) {
	var channels []*model.DmChannel
	for _, channel := range s.dmChannels {
		if channel.UserLo != userID && channel.UserHi != userID {
			continue
		}
		value := *channel
		channels = append(channels, &value)
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i].ID > channels[j].ID })
	return channels, nil
}

func (s *fakeStore) AckMessage(_ context.Context, userID, channelID, messageID int64) (bool, error) {
	message, ok := s.messages[messageID]
	if !ok || message.ChannelID != channelID {
		return false, sql.ErrNoRows
	}
	if s.readStates[userID] == nil {
		s.readStates[userID] = make(map[int64]int64)
	}
	if current, ok := s.readStates[userID][channelID]; !ok || messageID > current {
		s.readStates[userID][channelID] = messageID
		return true, nil
	}
	return false, nil
}

func (s *fakeStore) ListReadyChannelReadStates(_ context.Context, userID int64, channelIDs []int64) ([]*model.ChannelReadState, error) {
	s.listReadyCalls++
	s.readyBatchSizes = append(s.readyBatchSizes, len(channelIDs))
	var states []*model.ChannelReadState
	byChannel := s.readStates[userID]
	for _, channelID := range channelIDs {
		lastReadID := int64(0)
		if byChannel != nil {
			if v, ok := byChannel[channelID]; ok {
				lastReadID = v
			}
		}
		state := &model.ChannelReadState{
			UserID:            userID,
			ChannelID:         channelID,
			LastReadMessageID: lastReadID,
		}
		for _, message := range s.messages {
			if message.ChannelID != channelID || message.DeletedAt != 0 {
				continue
			}
			state.LastMessageID = max(state.LastMessageID, message.ID)
			if message.ID <= lastReadID {
				continue
			}
			if slices.Contains(s.mentions[message.ID].UserIDs, userID) {
				state.MentionCount++
			}
		}
		states = append(states, state)
	}
	return states, nil
}

func (s *fakeStore) GetLastMessageID(_ context.Context, channelID int64) (int64, error) {
	var lastMessageID int64
	for _, message := range s.messages {
		if message.ChannelID == channelID && message.DeletedAt == 0 {
			lastMessageID = max(lastMessageID, message.ID)
		}
	}
	return lastMessageID, nil
}

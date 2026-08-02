package server

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sort"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
)

func (s *fakeStore) CreateMessage(_ context.Context, params store.CreateMessageParams) (*model.Message, error) {
	if _, ok := s.messages[params.MessageID]; ok {
		return nil, errors.New("duplicate message")
	}
	message := &model.Message{
		ID:                  params.MessageID,
		ChannelID:           params.ChannelID,
		AuthorID:            params.AuthorID,
		Content:             params.Content,
		Type:                params.Type,
		Flags:               params.Flags,
		ReferencedMessageID: params.ReferencedMessageID,
		ReferencedChannelID: params.ReferencedChannelID,
		Attachments:         append([]model.Attachment(nil), params.Attachments...),
		CreatedAt:           1,
		Revision:            1,
	}
	s.messages[message.ID] = message
	return cloneMessage(message), nil
}

func (s *fakeStore) GetMessage(_ context.Context, messageID int64) (*model.Message, error) {
	if s.getMessageErr != nil {
		return nil, s.getMessageErr
	}
	message, ok := s.messages[messageID]
	if !ok || message.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	return cloneMessage(message), nil
}

func (s *fakeStore) ListMessages(_ context.Context, params store.ListMessagesParams) ([]*model.Message, error) {
	var messages []*model.Message
	for _, message := range s.messages {
		if message.ChannelID != params.ChannelID || message.DeletedAt != 0 {
			continue
		}
		if params.Before != 0 && message.ID >= params.Before {
			continue
		}
		if params.After != 0 && message.ID <= params.After {
			continue
		}
		messages = append(messages, cloneMessage(message))
	}
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].ID > messages[j].ID
	})
	if params.Limit > 0 && len(messages) > params.Limit {
		messages = messages[:params.Limit]
	}
	return messages, nil
}

func (s *fakeStore) UpdateMessage(_ context.Context, params store.UpdateMessageParams) (*model.Message, error) {
	message, ok := s.messages[params.MessageID]
	if !ok || message.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	if !params.HasModPermission && message.AuthorID != params.ActorUserID {
		return nil, store.ErrPermissionDenied
	}
	if params.Content != nil {
		message.Content = *params.Content
	}
	if params.Flags != nil {
		message.Flags = *params.Flags
	}
	if params.Attachments != nil {
		message.Attachments = append([]model.Attachment(nil), (*params.Attachments)...)
	}
	message.EditedAt = 2
	message.UpdatedAt = 2
	message.Revision++
	return cloneMessage(message), nil
}

func (s *fakeStore) DeleteMessage(_ context.Context, messageID, actorUserID int64, hasModPermission bool) (*model.Message, error) {
	message, ok := s.messages[messageID]
	if !ok || message.DeletedAt != 0 {
		return nil, sql.ErrNoRows
	}
	if !hasModPermission && message.AuthorID != actorUserID {
		return nil, store.ErrPermissionDenied
	}
	message.DeletedAt = 3
	message.UpdatedAt = 3
	message.Revision++
	return cloneMessage(message), nil
}

func (s *fakeStore) ReplaceMessageMentions(_ context.Context, messageID int64, mentions model.MessageMentions) error {
	value := model.MessageMentions{
		UserIDs:  append([]int64(nil), mentions.UserIDs...),
		RoleIDs:  append([]int64(nil), mentions.RoleIDs...),
		Everyone: mentions.Everyone,
	}
	slices.Sort(value.UserIDs)
	slices.Sort(value.RoleIDs)
	s.mentions[messageID] = value
	return nil
}

func (s *fakeStore) ListMessageMentions(_ context.Context, messageID int64) (*model.MessageMentions, error) {
	value := s.mentions[messageID]
	return &model.MessageMentions{
		UserIDs:  append([]int64(nil), value.UserIDs...),
		RoleIDs:  append([]int64(nil), value.RoleIDs...),
		Everyone: value.Everyone,
	}, nil
}

func (s *fakeStore) ListMessagesMentions(_ context.Context, messageIDs []int64) (map[int64]*model.MessageMentions, error) {
	byMessage := make(map[int64]*model.MessageMentions, len(messageIDs))
	for _, messageID := range messageIDs {
		value, err := s.ListMessageMentions(context.Background(), messageID)
		if err != nil {
			return nil, err
		}
		byMessage[messageID] = value
	}
	return byMessage, nil
}

func (s *fakeStore) RebuildExpandedMessageMentions(_ context.Context, messageID, expectedRevision int64, userIDs []int64) (bool, error) {
	if s.rebuildStale {
		return false, nil
	}
	message, ok := s.messages[messageID]
	if !ok || message.DeletedAt != 0 || message.Revision != expectedRevision {
		return false, nil
	}
	s.rebuildBatches = append(s.rebuildBatches, append([]int64(nil), userIDs...))
	value := s.mentions[messageID]
	value.UserIDs = append([]int64(nil), userIDs...)
	slices.Sort(value.UserIDs)
	s.mentions[messageID] = value
	return true, nil
}

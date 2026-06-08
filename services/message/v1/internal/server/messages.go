package server

import (
	"context"
	"log/slog"
	"time"

	messagev1 "github.com/soasurs/cordis/gen/message/v1"
	"github.com/soasurs/cordis/pkg/outbox"
	"github.com/soasurs/cordis/services/message/v1/internal/model"
	"github.com/soasurs/cordis/services/message/v1/internal/store"
	"github.com/twmb/franz-go/pkg/kgo"
)

func (s *messageServer) CreateMessage(ctx context.Context, req *messagev1.CreateMessageRequest) (*messagev1.CreateMessageResponse, error) {
	if req.GetChannelId() <= 0 {
		return nil, invalidRequest("channel id is required")
	}
	if req.GetAuthorId() <= 0 {
		return nil, invalidRequest("author id is required")
	}
	if err := validateContent(req.GetContent()); err != nil {
		return nil, err
	}
	attachments := toModelAttachments(req.GetAttachments())
	if err := validateAttachments(attachments); err != nil {
		return nil, err
	}
	if err := validateFlags(req.GetFlags()); err != nil {
		return nil, err
	}
	if req.GetContent() == "" && len(attachments) == 0 {
		return nil, invalidRequest("content or attachments are required")
	}

	messageType, err := normalizeMessageType(req.GetType())
	if err != nil {
		return nil, err
	}
	if messageType == messagev1.MessageType_MESSAGE_TYPE_REPLY && req.GetReferencedMessageId() <= 0 {
		return nil, invalidRequest("referenced message id is required")
	}
	if (req.GetReferencedMessageId() == 0) != (req.GetReferencedChannelId() == 0) {
		return nil, invalidRequest("referenced message and channel must be set together")
	}
	if err := validateMentionUserIDs(req.GetMentionUserIds()); err != nil {
		return nil, err
	}

	if req.GetReferencedMessageId() != 0 {
		referencedMessage, err := s.svcCtx.Store.GetMessage(ctx, req.GetReferencedMessageId())
		if err != nil {
			return nil, mapStoreError(err)
		}
		if referencedMessage.ChannelID != req.GetReferencedChannelId() {
			return nil, invalidRequest("referenced channel does not match referenced message")
		}
	}

	messageID := s.svcCtx.Snowflake.Generate().Int64()
	var created *model.Message
	var outboxEvent outbox.Event

	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		message, err := txStore.CreateMessage(ctx, store.CreateMessageParams{
			MessageID:           messageID,
			ChannelID:           req.GetChannelId(),
			AuthorID:            req.GetAuthorId(),
			Content:             req.GetContent(),
			Type:                int32(messageType),
			Flags:               req.GetFlags(),
			ReferencedMessageID: req.GetReferencedMessageId(),
			ReferencedChannelID: req.GetReferencedChannelId(),
			Attachments:         attachments,
		})
		if err != nil {
			return err
		}
		created = message

		if err := txStore.ReplaceMessageMentions(ctx, messageID, req.GetMentionUserIds()); err != nil {
			return err
		}

		// Build and enqueue the outbox event within the same transaction.
		outboxEvent, err = newMessageCreatedEvent(s.svcCtx.Cfg.Kafka.Topic, created)
		if err != nil {
			return err
		}
		return txStore.InsertOutboxEvent(ctx, outboxEvent)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	// Transaction committed — immediately flush the event to Kafka.
	s.flushOutboxEvent(outboxEvent.ID)

	resp := new(messagev1.CreateMessageResponse)
	resp.SetMessage(toPBMessage(created))
	return resp, nil
}

func (s *messageServer) UpdateMessage(ctx context.Context, req *messagev1.UpdateMessageRequest) (*messagev1.UpdateMessageResponse, error) {
	if req.GetMessageId() <= 0 {
		return nil, invalidRequest("message id is required")
	}
	if req.GetActorUserId() <= 0 {
		return nil, invalidRequest("actor user id is required")
	}
	if !req.HasContent() && !req.HasFlags() && !req.HasAttachments() && !req.HasMentions() {
		return nil, invalidRequest("at least one field must be updated")
	}

	params := store.UpdateMessageParams{
		MessageID:        req.GetMessageId(),
		ActorUserID:      req.GetActorUserId(),
		HasModPermission: req.GetHasPermission(),
	}
	if req.HasContent() {
		content := req.GetContent()
		if err := validateContent(content); err != nil {
			return nil, err
		}
		params.Content = &content
	}
	if req.HasFlags() {
		flags := req.GetFlags()
		if err := validateFlags(flags); err != nil {
			return nil, err
		}
		params.Flags = &flags
	}
	if req.HasAttachments() {
		attachments := toModelAttachments(req.GetAttachments().GetAttachments())
		if err := validateAttachments(attachments); err != nil {
			return nil, err
		}
		params.Attachments = &attachments
	}
	if req.HasMentions() {
		if err := validateMentionUserIDs(req.GetMentions().GetUserIds()); err != nil {
			return nil, err
		}
	}

	var updated *model.Message
	var outboxEvent outbox.Event

	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		message, err := txStore.UpdateMessage(ctx, params)
		if err != nil {
			return err
		}
		updated = message

		if req.HasMentions() {
			if err := txStore.ReplaceMessageMentions(ctx, req.GetMessageId(), req.GetMentions().GetUserIds()); err != nil {
				return err
			}
		}

		outboxEvent, err = newMessageUpdatedEvent(s.svcCtx.Cfg.Kafka.Topic, updated)
		if err != nil {
			return err
		}
		outboxEvent.ID = s.svcCtx.Snowflake.Generate().Int64()
		return txStore.InsertOutboxEvent(ctx, outboxEvent)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	s.flushOutboxEvent(outboxEvent.ID)

	resp := new(messagev1.UpdateMessageResponse)
	resp.SetMessage(toPBMessage(updated))
	return resp, nil
}

func (s *messageServer) DeleteMessage(ctx context.Context, req *messagev1.DeleteMessageRequest) (*messagev1.DeleteMessageResponse, error) {
	if req.GetMessageId() <= 0 {
		return nil, invalidRequest("message id is required")
	}
	if req.GetActorUserId() <= 0 {
		return nil, invalidRequest("actor user id is required")
	}

	// Fetch the message first to get the channel_id for the event.
	msg, err := s.svcCtx.Store.GetMessage(ctx, req.GetMessageId())
	if err != nil {
		return nil, mapStoreError(err)
	}

	var outboxEvent outbox.Event

	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		if err := txStore.DeleteMessage(ctx, req.GetMessageId(), req.GetActorUserId(), req.GetHasPermission()); err != nil {
			return err
		}

		var err error
		outboxEvent, err = newMessageDeletedEvent(s.svcCtx.Cfg.Kafka.Topic, msg.ID, msg.ChannelID)
		if err != nil {
			return err
		}
		outboxEvent.ID = s.svcCtx.Snowflake.Generate().Int64()
		return txStore.InsertOutboxEvent(ctx, outboxEvent)
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	s.flushOutboxEvent(outboxEvent.ID)

	resp := new(messagev1.DeleteMessageResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *messageServer) GetMessage(ctx context.Context, req *messagev1.GetMessageRequest) (*messagev1.GetMessageResponse, error) {
	if req.GetMessageId() <= 0 {
		return nil, invalidRequest("message id is required")
	}
	message, err := s.svcCtx.Store.GetMessage(ctx, req.GetMessageId())
	if err != nil {
		return nil, mapStoreError(err)
	}

	summaries, err := s.svcCtx.Store.ListReactionSummaries(ctx, []int64{message.ID}, req.GetViewerUserId())
	if err != nil {
		return nil, err
	}

	resp := new(messagev1.GetMessageResponse)
	resp.SetMessage(toPBMessage(message))
	resp.SetReactions(toPBReactionSummaries(summaries[message.ID]))
	return resp, nil
}

func (s *messageServer) ListMessages(ctx context.Context, req *messagev1.ListMessagesRequest) (*messagev1.ListMessagesResponse, error) {
	if req.GetChannelId() <= 0 {
		return nil, invalidRequest("channel id is required")
	}
	limit, err := normalizeLimit(req.GetLimit(), defaultMessageLimit, maxMessageLimit)
	if err != nil {
		return nil, err
	}

	params := store.ListMessagesParams{
		ChannelID: req.GetChannelId(),
		Limit:     limit,
	}
	switch {
	case req.HasBefore():
		if req.GetBefore() <= 0 {
			return nil, invalidRequest("before cursor must be positive")
		}
		params.Before = req.GetBefore()
	case req.HasAfter():
		if req.GetAfter() <= 0 {
			return nil, invalidRequest("after cursor must be positive")
		}
		params.After = req.GetAfter()
	case req.HasAround():
		if req.GetAround() <= 0 {
			return nil, invalidRequest("around cursor must be positive")
		}
		params.Around = req.GetAround()
	}

	messages, err := s.svcCtx.Store.ListMessages(ctx, params)
	if err != nil {
		return nil, err
	}
	messageIDs := make([]int64, 0, len(messages))
	for _, message := range messages {
		messageIDs = append(messageIDs, message.ID)
	}
	summaries, err := s.svcCtx.Store.ListReactionSummaries(ctx, messageIDs, req.GetViewerUserId())
	if err != nil {
		return nil, err
	}

	resp := new(messagev1.ListMessagesResponse)
	resp.SetMessages(toPBMessages(messages))
	resp.SetReactions(toPBReactionSummaryMap(summaries))
	setListCursors(resp, messages)
	return resp, nil
}

// flushOutboxEvent attempts to deliver a single outbox event immediately
// after the transaction committed. This is the happy path — the relay
// handles the cases where this goroutine fails or the process crashes.
func (s *messageServer) flushOutboxEvent(eventID int64) {
	if s.svcCtx.Kafka == nil {
		return
	}

	s.svcCtx.ShutdownWg.Add(1)
	go func() {
		defer s.svcCtx.ShutdownWg.Done()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// CAS claim the event — the relay may also try to claim it.
		// Whichever claims first wins.
		evt, err := s.svcCtx.Store.ClaimOutboxEvent(ctx, eventID, outbox.Now())
		if err != nil || evt == nil {
			return // already claimed by relay, or error
		}

		rec := &kgo.Record{
			Topic: evt.Topic,
			Key:   evt.Key,
			Value: evt.Payload,
		}

		results := s.svcCtx.Kafka.ProduceSync(ctx, rec)
		if err := results.FirstErr(); err != nil {
			slog.Error("flush outbox event failed",
				"event_id", eventID,
				"topic", evt.Topic,
				"error", err,
			)
			// Release back to pending so the relay picks it up.
			_ = s.svcCtx.Store.ReleaseOutboxEvent(ctx, evt.ID)
			return
		}

		// Published successfully — delete from outbox.
		_ = s.svcCtx.Store.DeleteOutboxEvent(ctx, evt.ID)
	}()
}

func toPBMessages(messages []*model.Message) []*messagev1.Message {
	values := make([]*messagev1.Message, 0, len(messages))
	for _, message := range messages {
		values = append(values, toPBMessage(message))
	}
	return values
}

func toPBReactionSummaries(summaries []*model.ReactionSummary) []*messagev1.ReactionSummary {
	values := make([]*messagev1.ReactionSummary, 0, len(summaries))
	for _, summary := range summaries {
		values = append(values, toPBReactionSummary(summary))
	}
	return values
}

func toPBReactionSummaryMap(summaries map[int64][]*model.ReactionSummary) map[int64]*messagev1.ReactionSummaryList {
	values := make(map[int64]*messagev1.ReactionSummaryList, len(summaries))
	for messageID, messageSummaries := range summaries {
		list := new(messagev1.ReactionSummaryList)
		list.SetReactions(toPBReactionSummaries(messageSummaries))
		values[messageID] = list
	}
	return values
}

func setListCursors(resp *messagev1.ListMessagesResponse, messages []*model.Message) {
	if len(messages) == 0 {
		return
	}
	minID := messages[0].ID
	maxID := messages[0].ID
	for _, message := range messages[1:] {
		if message.ID < minID {
			minID = message.ID
		}
		if message.ID > maxID {
			maxID = message.ID
		}
	}
	resp.SetBeforeCursor(minID)
	resp.SetAfterCursor(maxID)
}

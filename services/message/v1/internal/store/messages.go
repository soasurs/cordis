package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

type attachmentJSON struct {
	Key         string `json:"key"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	Width       int32  `json:"width"`
	Height      int32  `json:"height"`
}

type messageRow struct {
	ID                  int64         `db:"id"`
	ChannelID           int64         `db:"channel_id"`
	AuthorID            int64         `db:"author_id"`
	Content             string        `db:"content"`
	Type                int32         `db:"type"`
	Flags               int32         `db:"flags"`
	ReferencedMessageID sql.NullInt64 `db:"referenced_message_id"`
	ReferencedChannelID sql.NullInt64 `db:"referenced_channel_id"`
	Attachments         string        `db:"attachments"`
	EditedAt            sql.NullInt64 `db:"edited_at"`
	CreatedAt           int64         `db:"created_at"`
	UpdatedAt           int64         `db:"updated_at"`
	Revision            int64         `db:"revision"`
	DeletedAt           int64         `db:"deleted_at"`
}

type queryArgs struct {
	values []any
}

func (a *queryArgs) bind(value any) string {
	a.values = append(a.values, value)
	return "$" + strconv.Itoa(len(a.values))
}

func (s *SQLStore) CreateMessage(ctx context.Context, params CreateMessageParams) (*model.Message, error) {
	now := time.Now().UnixMilli()
	attachments, err := marshalAttachments(params.Attachments)
	if err != nil {
		return nil, err
	}

	row := &messageRow{
		ID:          params.MessageID,
		ChannelID:   params.ChannelID,
		AuthorID:    params.AuthorID,
		Content:     params.Content,
		Type:        params.Type,
		Flags:       params.Flags,
		Attachments: attachments,
		CreatedAt:   now,
		UpdatedAt:   0,
		Revision:    1,
		DeletedAt:   0,
	}
	if params.ReferencedMessageID != 0 {
		row.ReferencedMessageID = sql.NullInt64{Int64: params.ReferencedMessageID, Valid: true}
	}
	if params.ReferencedChannelID != 0 {
		row.ReferencedChannelID = sql.NullInt64{Int64: params.ReferencedChannelID, Valid: true}
	}

	rows, err := sqlx.NamedQueryContext(ctx, s.q, CreateMessageQuery, row)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	created := new(messageRow)
	if err := rows.StructScan(created); err != nil {
		return nil, err
	}
	return created.toModel()
}

func (s *SQLStore) GetMessage(ctx context.Context, messageID int64) (*model.Message, error) {
	row := new(messageRow)
	if err := sqlx.GetContext(ctx, s.q, row, GetMessageQuery, messageID, 0); err != nil {
		return nil, err
	}
	return row.toModel()
}

func (s *SQLStore) ListMessages(ctx context.Context, params ListMessagesParams) ([]*model.Message, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}
	switch {
	case params.Around != 0:
		return s.listMessagesAround(ctx, params)
	case params.Before != 0:
		return s.selectMessages(ctx, ListMessagesBeforeQuery, params.ChannelID, 0, params.Before, params.Limit)
	case params.After != 0:
		messages, err := s.selectMessages(ctx, ListMessagesAfterQuery, params.ChannelID, 0, params.After, params.Limit)
		if err != nil {
			return nil, err
		}
		reverseMessages(messages)
		return messages, nil
	default:
		return s.selectMessages(ctx, ListNewestMessagesQuery, params.ChannelID, 0, params.Limit)
	}
}

func (s *SQLStore) UpdateMessage(ctx context.Context, params UpdateMessageParams) (*model.Message, error) {
	query, args, err := buildUpdateMessageQuery(params, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}

	row := new(messageRow)
	if err := sqlx.GetContext(ctx, s.q, row, query, args...); err != nil {
		if err == sql.ErrNoRows {
			if params.HasModPermission {
				return nil, sql.ErrNoRows
			}
			return nil, s.noRowsForActor(ctx, params.MessageID)
		}
		return nil, err
	}
	return row.toModel()
}

func buildUpdateMessageQuery(params UpdateMessageParams, now int64) (string, []any, error) {
	args := new(queryArgs)
	updatedAt := args.bind(now)
	sets := []string{
		"updated_at = " + updatedAt,
		"edited_at = " + updatedAt,
		"revision = revision + 1",
	}

	if params.Content != nil {
		sets = append(sets, "content = "+args.bind(*params.Content))
	}
	if params.Flags != nil {
		sets = append(sets, "flags = "+args.bind(*params.Flags))
	}
	if params.Attachments != nil {
		attachments, err := marshalAttachments(*params.Attachments)
		if err != nil {
			return "", nil, err
		}
		sets = append(sets, "attachments = CAST("+args.bind(attachments)+" AS JSONB)")
	}

	conditions := []string{
		"id = " + args.bind(params.MessageID),
	}
	if !params.HasModPermission {
		conditions = append(conditions, "author_id = "+args.bind(params.ActorUserID))
	}
	conditions = append(conditions, "deleted_at = "+args.bind(int64(0)))

	query := fmt.Sprintf(`
	UPDATE
		messages
	SET
		%s
	WHERE
		%s
	RETURNING
		%s
	`, strings.Join(sets, ",\n\t\t"), strings.Join(conditions, "\n\tAND\n\t\t"), messageColumns)
	return query, args.values, nil
}

func (s *SQLStore) DeleteMessage(ctx context.Context, messageID, actorUserID int64, hasModPermission bool) (*model.Message, error) {
	now := time.Now().UnixMilli()
	row := new(messageRow)
	if hasModPermission {
		if err := sqlx.GetContext(ctx, s.q, row, DeleteMessageModStatement, now, messageID, int64(0)); err != nil {
			return nil, err
		}
	} else {
		if err := sqlx.GetContext(ctx, s.q, row, DeleteMessageStatement, now, messageID, actorUserID, int64(0)); err != nil {
			if err == sql.ErrNoRows {
				return nil, s.noRowsForActor(ctx, messageID)
			}
			return nil, err
		}
	}
	return row.toModel()
}

func (s *SQLStore) ReplaceMessageMentions(ctx context.Context, messageID int64, userIDs []int64) error {
	if _, err := s.q.ExecContext(ctx, DeleteMessageMentionsStatement, messageID); err != nil {
		return err
	}
	ids := uniquePositiveIDs(userIDs)
	if len(ids) == 0 {
		return nil
	}
	return s.batchInsertMentions(ctx, messageID, ids)
}

func (s *SQLStore) batchInsertMentions(ctx context.Context, messageID int64, userIDs []int64) error {
	_, err := s.q.ExecContext(ctx, InsertMessageMentionsStatement, messageID, pq.Array(userIDs))
	return err
}

func (s *SQLStore) ListMentionUserIDs(ctx context.Context, messageID int64) ([]int64, error) {
	var userIDs []int64
	if err := sqlx.SelectContext(ctx, s.q, &userIDs, ListMessageMentionsQuery, messageID); err != nil {
		return nil, err
	}
	return userIDs, nil
}

func (s *SQLStore) selectMessages(ctx context.Context, query string, args ...any) ([]*model.Message, error) {
	var rows []messageRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, query, args...); err != nil {
		return nil, err
	}
	return messageRowsToModels(rows)
}

func (s *SQLStore) listMessagesAround(ctx context.Context, params ListMessagesParams) ([]*model.Message, error) {
	older, err := s.selectMessages(ctx, ListMessagesAroundOlderQuery, params.ChannelID, 0, params.Around, params.Limit)
	if err != nil {
		return nil, err
	}
	newer, err := s.selectMessages(ctx, ListMessagesAroundNewerQuery, params.ChannelID, 0, params.Around, params.Limit)
	if err != nil {
		return nil, err
	}
	reverseMessages(newer)

	all := append(newer, older...)
	if len(all) <= params.Limit {
		return all, nil
	}

	anchorIdx := len(newer)
	half := params.Limit / 2
	start := max(anchorIdx-half, 0)
	end := start + params.Limit
	if end > len(all) {
		end = len(all)
		start = max(end-params.Limit, 0)
	}
	return all[start:end], nil
}

func (s *SQLStore) noRowsForActor(ctx context.Context, messageID int64) error {
	var exists bool
	if err := sqlx.GetContext(ctx, s.q, &exists, CheckMessageExistsQuery, messageID, 0); err != nil {
		return err
	}
	if exists {
		return ErrPermissionDenied
	}
	return sql.ErrNoRows
}

func messageRowsToModels(rows []messageRow) ([]*model.Message, error) {
	messages := make([]*model.Message, 0, len(rows))
	for i := range rows {
		message, err := rows[i].toModel()
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func (r *messageRow) toModel() (*model.Message, error) {
	attachments, err := unmarshalAttachments(r.Attachments)
	if err != nil {
		return nil, err
	}
	message := &model.Message{
		ID:          r.ID,
		ChannelID:   r.ChannelID,
		AuthorID:    r.AuthorID,
		Content:     r.Content,
		Type:        r.Type,
		Flags:       r.Flags,
		Attachments: attachments,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		Revision:    r.Revision,
		DeletedAt:   r.DeletedAt,
	}
	if r.ReferencedMessageID.Valid {
		message.ReferencedMessageID = r.ReferencedMessageID.Int64
	}
	if r.ReferencedChannelID.Valid {
		message.ReferencedChannelID = r.ReferencedChannelID.Int64
	}
	if r.EditedAt.Valid {
		message.EditedAt = r.EditedAt.Int64
	}
	return message, nil
}

func marshalAttachments(attachments []model.Attachment) (string, error) {
	values := make([]attachmentJSON, 0, len(attachments))
	for _, attachment := range attachments {
		values = append(values, attachmentJSON{
			Key:         attachment.Key,
			Filename:    attachment.Filename,
			Size:        attachment.Size,
			ContentType: attachment.ContentType,
			Width:       attachment.Width,
			Height:      attachment.Height,
		})
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalAttachments(value string) ([]model.Attachment, error) {
	if value == "" {
		return nil, nil
	}
	var attachments []attachmentJSON
	if err := json.Unmarshal([]byte(value), &attachments); err != nil {
		return nil, err
	}
	values := make([]model.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		values = append(values, model.Attachment{
			Key:         attachment.Key,
			Filename:    attachment.Filename,
			Size:        attachment.Size,
			ContentType: attachment.ContentType,
			Width:       attachment.Width,
			Height:      attachment.Height,
		})
	}
	return values, nil
}

func reverseMessages(messages []*model.Message) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}

func uniquePositiveIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	values := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		values = append(values, id)
	}
	slices.Sort(values)
	return values
}

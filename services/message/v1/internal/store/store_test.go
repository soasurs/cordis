package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

func TestMarshalAttachmentsRoundTrip(t *testing.T) {
	attachments := []model.Attachment{
		{
			Key:         "attachments/1/a.png",
			Filename:    "a.png",
			Size:        10,
			ContentType: "image/png",
			Width:       100,
			Height:      200,
		},
	}

	value, err := marshalAttachments(attachments)
	require.NoError(t, err)

	got, err := unmarshalAttachments(value)
	require.NoError(t, err)
	require.Equal(t, attachments, got)
}

func TestUniquePositiveIDs(t *testing.T) {
	got := uniquePositiveIDs([]int64{3, 0, 2, 3, -1, 1})
	require.Equal(t, []int64{1, 2, 3}, got)
}

func TestBuildUpdateMessageQuery(t *testing.T) {
	content := "updated'); DROP TABLE messages; --"
	flags := int32(1)
	attachments := []model.Attachment{
		{Key: "attachments/1/a.png", Filename: "a.png", Size: 10},
	}

	query, args, err := buildUpdateMessageQuery(UpdateMessageParams{
		MessageID:   100,
		ActorUserID: 20,
		Content:     &content,
		Flags:       &flags,
		Attachments: &attachments,
	}, 1234)
	require.NoError(t, err)

	require.Contains(t, query, "updated_at = $1")
	require.Contains(t, query, "edited_at = $1")
	require.Contains(t, query, "content = $2")
	require.Contains(t, query, "flags = $3")
	require.Contains(t, query, "attachments = CAST($4 AS JSONB)")
	require.Contains(t, query, "id = $5")
	require.Contains(t, query, "author_id = $6")
	require.Contains(t, query, "deleted_at = $7")
	require.NotContains(t, query, content)
	require.Equal(t, []any{
		int64(1234),
		content,
		flags,
		`[{"key":"attachments/1/a.png","filename":"a.png","size":10,"content_type":"","width":0,"height":0}]`,
		int64(100),
		int64(20),
		int64(0),
	}, args)
}

func TestBuildUpdateMessageQueryWithModPermission(t *testing.T) {
	content := "updated"

	query, args, err := buildUpdateMessageQuery(UpdateMessageParams{
		MessageID:        100,
		ActorUserID:      20,
		HasModPermission: true,
		Content:          &content,
	}, 1234)
	require.NoError(t, err)

	require.NotContains(t, query, "\n\t\tauthor_id =")
	require.Contains(t, query, "id = $3")
	require.Contains(t, query, "deleted_at = $4")
	require.Equal(t, []any{int64(1234), content, int64(100), int64(0)}, args)
}

func TestInsertMessageMentionsStatementIsStatic(t *testing.T) {
	require.Contains(t, InsertMessageMentionsStatement, "unnest($2::BIGINT[])")
	require.NotContains(t, strings.ToUpper(InsertMessageMentionsStatement), "VALUES")
}

package store

import (
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

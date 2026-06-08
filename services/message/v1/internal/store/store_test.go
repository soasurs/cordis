package store

import (
	"reflect"
	"testing"

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
	if err != nil {
		t.Fatalf("marshalAttachments returned error: %v", err)
	}
	got, err := unmarshalAttachments(value)
	if err != nil {
		t.Fatalf("unmarshalAttachments returned error: %v", err)
	}
	if !reflect.DeepEqual(got, attachments) {
		t.Fatalf("attachments = %+v, want %+v", got, attachments)
	}
}

func TestUniquePositiveIDs(t *testing.T) {
	got := uniquePositiveIDs([]int64{3, 0, 2, 3, -1, 1})
	want := []int64{1, 2, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("uniquePositiveIDs() = %v, want %v", got, want)
	}
}

package server

import (
	"context"

	"github.com/soasurs/cordis/pkg/outbox"
)

func (s *fakeStore) EnsureMessageStream(_ context.Context, _ string, _ int) error {
	return nil
}

func (s *fakeStore) ReserveMessageSequences(_ context.Context, _ string, count int) (outbox.ReservedRange, error) {
	first := int64(len(s.messageOutbox) + 1)
	return outbox.ReservedRange{
		FirstSequence: first,
		LastSequence:  first + int64(count) - 1,
		ShardID:       0,
	}, nil
}

func (s *fakeStore) InsertMessageOutbox(_ context.Context, records []outbox.Record) error {
	s.messageOutbox = append(s.messageOutbox, records...)
	return nil
}

func (s *fakeStore) EnsureReadStateStream(_ context.Context, _ string, _ int) error {
	return nil
}

func (s *fakeStore) ReserveReadStateSequences(_ context.Context, _ string, count int) (outbox.ReservedRange, error) {
	first := int64(len(s.readStateOutbox) + 1)
	return outbox.ReservedRange{
		FirstSequence: first,
		LastSequence:  first + int64(count) - 1,
		ShardID:       0,
	}, nil
}

func (s *fakeStore) InsertReadStateOutbox(_ context.Context, records []outbox.Record) error {
	s.readStateOutbox = append(s.readStateOutbox, records...)
	return nil
}

func (s *fakeStore) NotifyOutbox(_ context.Context, _ string) error {
	return nil
}

package server

import (
	"context"

	"github.com/soasurs/cordis/pkg/outbox"
)

func (s *fakeStore) EnsureUserStream(_ context.Context, _ string, _ int) error {
	return nil
}

func (s *fakeStore) ReserveUserSequences(_ context.Context, streamKey string, count int) (outbox.ReservedRange, error) {
	first := s.userStreamSequences[streamKey] + 1
	s.userStreamSequences[streamKey] += int64(count)
	return outbox.ReservedRange{
		FirstSequence: first,
		LastSequence:  first + int64(count) - 1,
		ShardID:       0,
	}, nil
}

func (s *fakeStore) InsertUserOutbox(_ context.Context, records []outbox.Record) error {
	s.userOutbox = append(s.userOutbox, records...)
	return nil
}

func (s *fakeStore) NotifyOutbox(_ context.Context, _ string) error {
	return nil
}

func (s *fakeStore) resetOutbox() {
	s.userOutbox = nil
	s.userStreamSequences = make(map[string]int64)
}

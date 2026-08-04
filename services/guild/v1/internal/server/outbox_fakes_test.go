package server

import (
	"context"

	"github.com/soasurs/cordis/pkg/outbox"
)

func (s *fakeStore) EnsureGuildStream(_ context.Context, _ string, _ int) error {
	return nil
}

func (s *fakeStore) ReserveGuildSequences(_ context.Context, streamKey string, count int) (outbox.ReservedRange, error) {
	first := s.guildStreamSequences[streamKey] + 1
	s.guildStreamSequences[streamKey] += int64(count)
	return outbox.ReservedRange{
		FirstSequence: first,
		LastSequence:  first + int64(count) - 1,
		ShardID:       0,
	}, nil
}

func (s *fakeStore) InsertGuildOutbox(_ context.Context, records []outbox.Record) error {
	s.guildOutbox = append(s.guildOutbox, records...)
	s.outboxCalls++
	return nil
}

func (s *fakeStore) NotifyOutbox(_ context.Context, _ string) error {
	return nil
}

func (s *fakeStore) rollbackOutbox(start, callsStart int, sequenceSnapshot map[string]int64) {
	s.guildOutbox = s.guildOutbox[:start]
	s.outboxCalls = callsStart
	s.guildStreamSequences = sequenceSnapshot
}

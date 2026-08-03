package kafka

import (
	"context"
	"encoding/json"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/soasurs/cordis/pkg/observability"
)

// MarshalTraceContext serializes the trace context that would be injected for
// one Kafka record. The outbox writer persists this so the relay can restore
// the original producer trace instead of replacing it with the relay's own.
func MarshalTraceContext(ctx context.Context) []byte {
	var headers []kgo.RecordHeader
	carrier := NewRecordHeaderCarrier(&headers)
	observability.KafkaPropagator().Inject(ctx, carrier)
	if len(headers) == 0 {
		return nil
	}
	data, err := json.Marshal(headers)
	if err != nil {
		return nil
	}
	return data
}

// UnmarshalTraceContext restores persisted trace headers on a record. Invalid
// or empty data is ignored so legacy rows still publish without tracing.
func UnmarshalTraceContext(data []byte, record *kgo.Record) {
	if len(data) == 0 || record == nil {
		return
	}
	var headers []kgo.RecordHeader
	if err := json.Unmarshal(data, &headers); err != nil {
		return
	}
	record.Headers = append(record.Headers, headers...)
}

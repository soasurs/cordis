package kafka

import (
	"context"
	"strings"

	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/soasurs/cordis/pkg/observability"
)

// RecordHeaderCarrier adapts Kafka record headers to an OpenTelemetry
// TextMapCarrier. Use [NewRecordHeaderCarrier] so Set can resize the header
// slice while replacing duplicate keys.
type RecordHeaderCarrier struct {
	headers *[]kgo.RecordHeader
}

// NewRecordHeaderCarrier wraps headers for trace-context injection or extraction.
func NewRecordHeaderCarrier(headers *[]kgo.RecordHeader) *RecordHeaderCarrier {
	return &RecordHeaderCarrier{headers: headers}
}

// Get returns the last value for key, matching Kafka's last-header convention.
func (c *RecordHeaderCarrier) Get(key string) string {
	if c == nil || c.headers == nil {
		return ""
	}
	for i := len(*c.headers) - 1; i >= 0; i-- {
		header := (*c.headers)[i]
		if strings.EqualFold(header.Key, key) {
			return string(header.Value)
		}
	}
	return ""
}

// Set replaces all existing values for key with one value.
func (c *RecordHeaderCarrier) Set(key, value string) {
	if c == nil || c.headers == nil {
		return
	}
	headers := *c.headers
	kept := headers[:0]
	for _, header := range headers {
		if !strings.EqualFold(header.Key, key) {
			kept = append(kept, header)
		}
	}
	*c.headers = append(kept, kgo.RecordHeader{Key: key, Value: []byte(value)})
}

// Keys returns the distinct header keys in their original order.
func (c *RecordHeaderCarrier) Keys() []string {
	if c == nil || c.headers == nil {
		return nil
	}
	keys := make([]string, 0, len(*c.headers))
	for _, header := range *c.headers {
		duplicate := false
		for _, key := range keys {
			if strings.EqualFold(key, header.Key) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			keys = append(keys, header.Key)
		}
	}
	return keys
}

// InjectTraceContext writes W3C trace context, but not baggage, to record.
func InjectTraceContext(ctx context.Context, record *kgo.Record) {
	if record == nil {
		return
	}
	observability.KafkaPropagator().Inject(ctx, NewRecordHeaderCarrier(&record.Headers))
}

// ExtractTraceContext reads W3C trace context from record into ctx.
func ExtractTraceContext(ctx context.Context, record *kgo.Record) context.Context {
	if record == nil {
		return ctx
	}
	return observability.KafkaPropagator().Extract(ctx, NewRecordHeaderCarrier(&record.Headers))
}

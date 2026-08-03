package server

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFinalizeEventInjectsSequenceAndDeliveryIndex(t *testing.T) {
	payload := []byte(`{"t":"message.created","d":{},"idempotency_key":"7"}`)
	finalized, err := finalizeEvent(payload, 42, 1)
	require.NoError(t, err)

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(finalized, &envelope))
	require.Equal(t, float64(42), envelope["stream_sequence"])
	require.Equal(t, float64(1), envelope["delivery_index"])
	require.Equal(t, "7", envelope["idempotency_key"])
}

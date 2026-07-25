package cursor

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const testSecret = "test-cursor-secret-at-least-32-bytes!"

type idPayload struct {
	ID int64 `json:"id"`
}

type timeIDPayload struct {
	Time int64 `json:"t"`
	ID   int64 `json:"i"`
}

func testCodec(t *testing.T) *Codec {
	t.Helper()
	c, err := NewCodec(testSecret)
	require.NoError(t, err)
	return c
}

func TestNewCodecRejectsShortSecret(t *testing.T) {
	_, err := NewCodec("too-short")
	require.Error(t, err)
}

func TestRoundTrip(t *testing.T) {
	c := testCodec(t)
	token, err := c.Encode(KindGuildMembers, timeIDPayload{Time: 1_700_000_000_000, ID: 99})
	require.NoError(t, err)
	require.NotEmpty(t, token)

	payload, ok, err := Decode[timeIDPayload](c, KindGuildMembers, token)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, int64(1_700_000_000_000), payload.Time)
	require.Equal(t, int64(99), payload.ID)
}

func TestDecodeEmpty(t *testing.T) {
	c := testCodec(t)
	_, ok, err := Decode[idPayload](c, KindUserGuilds, "")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDecodeRejectsTampering(t *testing.T) {
	c := testCodec(t)
	token, err := c.Encode(KindUserGuilds, idPayload{ID: 42})
	require.NoError(t, err)

	_, ok, err := Decode[idPayload](c, KindUserGuilds, token+"x")
	require.ErrorIs(t, err, ErrInvalid)
	require.False(t, ok)

	_, ok, err = Decode[idPayload](c, KindGuildInvites, token)
	require.ErrorIs(t, err, ErrInvalid)
	require.False(t, ok)

	other, err := NewCodec("other-cursor-secret-at-least-32-bytes!")
	require.NoError(t, err)
	_, ok, err = Decode[idPayload](other, KindUserGuilds, token)
	require.ErrorIs(t, err, ErrInvalid)
	require.False(t, ok)
}

func TestDecodeRejectsMalformed(t *testing.T) {
	c := testCodec(t)
	_, ok, err := Decode[idPayload](c, KindUserGuilds, "not-a-cursor")
	require.ErrorIs(t, err, ErrInvalid)
	require.False(t, ok)

	_, ok, err = Decode[idPayload](c, KindUserGuilds, "a.b")
	require.ErrorIs(t, err, ErrInvalid)
	require.False(t, ok)
}

func TestTrim(t *testing.T) {
	page, hasMore := Trim([]int{1, 2, 3}, 2)
	require.Equal(t, []int{1, 2}, page)
	require.True(t, hasMore)

	page, hasMore = Trim([]int{1, 2}, 2)
	require.Equal(t, []int{1, 2}, page)
	require.False(t, hasMore)
}

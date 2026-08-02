//go:build integration

package store

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/soasurs/cordis/services/message/v1/internal/model"
)

func testTransactRollback(t *testing.T, store Store) {
	const channelID = int64(2006)
	ctx := t.Context()

	require.NoError(t, store.Transact(ctx, func(tx Store) error {
		if _, err := tx.CreateMessage(ctx, CreateMessageParams{
			MessageID: 5501, ChannelID: channelID, AuthorID: 3001,
			Content: "committed", Type: 1,
		}); err != nil {
			return err
		}
		return tx.ReplaceMessageMentions(ctx, 5501, model.MessageMentions{UserIDs: []int64{4001}})
	}))
	loaded, err := store.GetMessage(ctx, 5501)
	require.NoError(t, err)
	require.Equal(t, "committed", loaded.Content)

	err = store.Transact(ctx, func(tx Store) error {
		if _, err := tx.CreateMessage(ctx, CreateMessageParams{
			MessageID: 5502, ChannelID: channelID, AuthorID: 3001,
			Content: "rollback", Type: 1,
		}); err != nil {
			return err
		}
		return errors.New("force rollback")
	})
	require.Error(t, err)
	_, err = store.GetMessage(ctx, 5502)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func testConstraintEnforcement(t *testing.T, store Store) {
	const channelID = int64(2007)
	ctx := t.Context()

	_, err := store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5601, ChannelID: channelID, AuthorID: 3001,
		Content: "", Type: 1,
	})
	requireCheckViolation(t, err)

	_, err = store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5602, ChannelID: channelID, AuthorID: 3001,
		Content: "m", Type: 2,
	})
	requireCheckViolation(t, err)

	_, err = store.CreateMessage(ctx, CreateMessageParams{
		MessageID: 5603, ChannelID: channelID, AuthorID: 3001,
		Content: "reply without reference", Type: 19,
	})
	requireCheckViolation(t, err)
}

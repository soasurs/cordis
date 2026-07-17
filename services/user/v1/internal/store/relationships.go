package store

import (
	"context"
	"database/sql"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"

	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

type relationshipRow struct {
	UserID    int64 `db:"user_id"`
	TargetID  int64 `db:"target_id"`
	Type      int16 `db:"type"`
	CreatedAt int64 `db:"created_at"`
	UpdatedAt int64 `db:"updated_at"`
}

func (s *SQLStore) UpsertRelationship(ctx context.Context, relationship *model.Relationship) error {
	_, err := s.q.ExecContext(
		ctx,
		UpsertRelationshipStatement,
		relationship.UserID,
		relationship.TargetID,
		relationship.Type,
		relationship.CreatedAt,
	)
	return err
}

func (s *SQLStore) GetRelationship(ctx context.Context, userID, targetID int64) (*model.Relationship, error) {
	row := new(relationshipRow)
	if err := sqlx.GetContext(ctx, s.q, row, GetRelationshipQuery, userID, targetID); err != nil {
		return nil, err
	}
	return relationshipFromRow(row), nil
}

func (s *SQLStore) DeleteRelationship(ctx context.Context, userID, targetID int64) error {
	res, err := s.q.ExecContext(ctx, DeleteRelationshipStatement, userID, targetID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteRelationshipExceptBlocked removes the reverse row of a mutation
// without ever clearing the other user's block.
func (s *SQLStore) DeleteRelationshipExceptBlocked(ctx context.Context, userID, targetID int64) error {
	_, err := s.q.ExecContext(ctx, DeleteRelationshipExceptBlockedStatement, userID, targetID)
	return err
}

func (s *SQLStore) ListRelationships(ctx context.Context, params ListRelationshipsParams) ([]*model.Relationship, error) {
	var rows []*relationshipRow
	if err := sqlx.SelectContext(
		ctx,
		s.q,
		&rows,
		ListRelationshipsQuery,
		params.UserID,
		params.Type,
		params.BeforeTargetID,
		params.Limit,
	); err != nil {
		return nil, err
	}
	relationships := make([]*model.Relationship, 0, len(rows))
	for _, row := range rows {
		relationships = append(relationships, relationshipFromRow(row))
	}
	return relationships, nil
}

func (s *SQLStore) ListRelationshipsByTargets(ctx context.Context, userID int64, targetIDs []int64) ([]*model.Relationship, error) {
	var rows []*relationshipRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, ListRelationshipsByTargetsQuery, userID, pq.Array(targetIDs)); err != nil {
		return nil, err
	}
	relationships := make([]*model.Relationship, 0, len(rows))
	for _, row := range rows {
		relationships = append(relationships, relationshipFromRow(row))
	}
	return relationships, nil
}

// ListRelationshipsBidirectional returns rows in both directions between
// userID and each target from a single statement, so callers get one
// consistent snapshot for block checks.
func (s *SQLStore) ListRelationshipsBidirectional(ctx context.Context, userID int64, targetIDs []int64) ([]*model.Relationship, error) {
	var rows []*relationshipRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, ListRelationshipsBidirectionalQuery, userID, pq.Array(targetIDs)); err != nil {
		return nil, err
	}
	relationships := make([]*model.Relationship, 0, len(rows))
	for _, row := range rows {
		relationships = append(relationships, relationshipFromRow(row))
	}
	return relationships, nil
}

func relationshipFromRow(row *relationshipRow) *model.Relationship {
	return &model.Relationship{
		UserID:    row.UserID,
		TargetID:  row.TargetID,
		Type:      row.Type,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

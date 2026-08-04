package store

import (
	"context"
	"database/sql"

	"github.com/jackc/pgx/v5"

	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

type relationshipRow struct {
	UserID    int64 `db:"user_id"`
	TargetID  int64 `db:"target_id"`
	Type      int16 `db:"type"`
	CreatedAt int64 `db:"created_at"`
	UpdatedAt int64 `db:"updated_at"`
}

// LockRelationshipPair serializes mutations involving either user. Callers
// must invoke it inside a transaction before reading relationship state.
func (s *SQLStore) LockRelationshipPair(ctx context.Context, userID, targetID int64) error {
	if targetID < userID {
		userID, targetID = targetID, userID
	}
	if _, err := s.q.Exec(ctx, LockRelationshipUserStatement, userID); err != nil {
		return err
	}
	_, err := s.q.Exec(ctx, LockRelationshipUserStatement, targetID)
	return err
}

func (s *SQLStore) UpsertRelationship(ctx context.Context, relationship *model.Relationship) error {
	_, err := s.q.Exec(
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
	row, err := scanOne(ctx, s.q, GetRelationshipQuery, pgx.RowToStructByName[relationshipRow], userID, targetID)
	if err != nil {
		return nil, err
	}
	return relationshipFromRow(&row), nil
}

func (s *SQLStore) DeleteRelationship(ctx context.Context, userID, targetID int64) error {
	tag, err := s.q.Exec(ctx, DeleteRelationshipStatement, userID, targetID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteRelationshipExceptBlocked removes the reverse row of a mutation
// without ever clearing the other user's block.
func (s *SQLStore) DeleteRelationshipExceptBlocked(ctx context.Context, userID, targetID int64) error {
	_, err := s.q.Exec(ctx, DeleteRelationshipExceptBlockedStatement, userID, targetID)
	return err
}

func (s *SQLStore) ListRelationships(ctx context.Context, params ListRelationshipsParams) ([]*model.Relationship, error) {
	rows, err := scanMany(
		ctx,
		s.q,
		ListRelationshipsQuery,
		pgx.RowToStructByName[relationshipRow],
		params.UserID,
		params.Type,
		params.BeforeCreatedAt,
		params.BeforeTargetID,
		params.Limit,
	)
	if err != nil {
		return nil, err
	}
	relationships := make([]*model.Relationship, 0, len(rows))
	for i := range rows {
		relationships = append(relationships, relationshipFromRow(&rows[i]))
	}
	return relationships, nil
}

func (s *SQLStore) ListRelationshipsByTargets(ctx context.Context, userID int64, targetIDs []int64) ([]*model.Relationship, error) {
	rows, err := scanMany(ctx, s.q, ListRelationshipsByTargetsQuery, pgx.RowToStructByName[relationshipRow], userID, targetIDs)
	if err != nil {
		return nil, err
	}
	relationships := make([]*model.Relationship, 0, len(rows))
	for i := range rows {
		relationships = append(relationships, relationshipFromRow(&rows[i]))
	}
	return relationships, nil
}

// ListRelationshipsBidirectional returns rows in both directions between
// userID and each target from a single statement, so callers get one
// consistent snapshot for block checks.
func (s *SQLStore) ListRelationshipsBidirectional(ctx context.Context, userID int64, targetIDs []int64) ([]*model.Relationship, error) {
	rows, err := scanMany(ctx, s.q, ListRelationshipsBidirectionalQuery, pgx.RowToStructByName[relationshipRow], userID, targetIDs)
	if err != nil {
		return nil, err
	}
	relationships := make([]*model.Relationship, 0, len(rows))
	for i := range rows {
		relationships = append(relationships, relationshipFromRow(&rows[i]))
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

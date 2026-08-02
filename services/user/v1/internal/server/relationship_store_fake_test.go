package server

import (
	"context"
	"database/sql"
	"sort"

	"github.com/soasurs/cordis/services/user/v1/internal/model"
	"github.com/soasurs/cordis/services/user/v1/internal/store"
)

func (s *fakeStore) UpsertRelationship(_ context.Context, rel *model.Relationship) error {
	key := [2]int64{rel.UserID, rel.TargetID}
	if existing, ok := s.relationships[key]; ok {
		existing.Type = rel.Type
		existing.UpdatedAt = rel.CreatedAt
	} else {
		s.relationships[key] = &model.Relationship{
			UserID:    rel.UserID,
			TargetID:  rel.TargetID,
			Type:      rel.Type,
			CreatedAt: rel.CreatedAt,
			UpdatedAt: 0,
		}
	}
	return nil
}

func (s *fakeStore) GetRelationship(_ context.Context, userID, targetID int64) (*model.Relationship, error) {
	key := [2]int64{userID, targetID}
	if rel, ok := s.relationships[key]; ok {
		return rel, nil
	}
	return nil, sql.ErrNoRows
}

func (s *fakeStore) DeleteRelationship(_ context.Context, userID, targetID int64) error {
	key := [2]int64{userID, targetID}
	if _, ok := s.relationships[key]; !ok {
		return sql.ErrNoRows
	}
	delete(s.relationships, key)
	return nil
}

func (s *fakeStore) DeleteRelationshipExceptBlocked(_ context.Context, userID, targetID int64) error {
	key := [2]int64{userID, targetID}
	if rel, ok := s.relationships[key]; ok && rel.Type != model.RelationshipBlocked {
		delete(s.relationships, key)
	}
	return nil
}

func (s *fakeStore) ListRelationships(_ context.Context, params store.ListRelationshipsParams) ([]*model.Relationship, error) {
	var result []*model.Relationship
	for key, rel := range s.relationships {
		if key[0] != params.UserID {
			continue
		}
		if params.Type != 0 && rel.Type != params.Type {
			continue
		}
		if params.BeforeCreatedAt != 0 {
			if rel.CreatedAt > params.BeforeCreatedAt ||
				(rel.CreatedAt == params.BeforeCreatedAt && rel.TargetID >= params.BeforeTargetID) {
				continue
			}
		}
		result = append(result, rel)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt != result[j].CreatedAt {
			return result[i].CreatedAt > result[j].CreatedAt
		}
		return result[i].TargetID > result[j].TargetID
	})
	if len(result) > params.Limit {
		result = result[:params.Limit]
	}
	return result, nil
}

func (s *fakeStore) ListRelationshipsBidirectional(_ context.Context, userID int64, targetIDs []int64) ([]*model.Relationship, error) {
	var relationships []*model.Relationship
	for _, targetID := range targetIDs {
		if relationship, ok := s.relationships[[2]int64{userID, targetID}]; ok {
			value := *relationship
			relationships = append(relationships, &value)
		}
		if relationship, ok := s.relationships[[2]int64{targetID, userID}]; ok {
			value := *relationship
			relationships = append(relationships, &value)
		}
	}
	return relationships, nil
}

func (s *fakeStore) ListRelationshipsByTargets(_ context.Context, userID int64, targetIDs []int64) ([]*model.Relationship, error) {
	targetSet := make(map[int64]bool, len(targetIDs))
	for _, id := range targetIDs {
		targetSet[id] = true
	}
	var result []*model.Relationship
	for key, rel := range s.relationships {
		if key[0] == userID && targetSet[key[1]] {
			result = append(result, rel)
		}
	}
	return result, nil
}

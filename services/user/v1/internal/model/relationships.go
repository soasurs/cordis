package model

// Relationship type values match user.v1.RelationshipType and the stored
// smallint representation.
const (
	RelationshipOutgoing int16 = 1
	RelationshipIncoming int16 = 2
	RelationshipFriend   int16 = 3
	RelationshipBlocked  int16 = 4
)

// Relationship is one user's directed view of their link to another user.
type Relationship struct {
	UserID    int64 `json:"user_id"`
	TargetID  int64 `json:"target_id"`
	Type      int16 `json:"type"`
	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

-- Align relationship list pagination with created_at keyset ordering.
-- Leading with created_at (not type) so unfiltered lists can use the index
-- order; type filters apply as a residual predicate.

DROP INDEX IF EXISTS user_relationships_user_type_idx;

CREATE INDEX IF NOT EXISTS user_relationships_user_created_idx
    ON user_relationships (user_id, created_at DESC, target_id DESC);

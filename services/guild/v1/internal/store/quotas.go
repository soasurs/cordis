package store

import (
	"context"
	"errors"
	"fmt"
)

const lockQuotaScopeStatement = `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`

// LockGuildChannelMutations serializes structural channel changes in one Guild.
func (s *SQLStore) LockGuildChannelMutations(ctx context.Context, guildID int64) error {
	if guildID <= 0 {
		return errors.New("guild id must be positive")
	}
	_, err := s.q.ExecContext(ctx, lockQuotaScopeStatement, guildChannelMutationLockKey(guildID))
	return err
}

func guildChannelMutationLockKey(guildID int64) string {
	return fmt.Sprintf("cordis:guild:quota:%s:%d", QuotaGuildChannels, guildID)
}

func (s *SQLStore) CheckResourceQuota(ctx context.Context, quota ResourceQuota) error {
	if quota.ScopeID <= 0 {
		return errors.New("quota scope id must be positive")
	}
	if quota.Limit <= 0 {
		return errors.New("quota limit must be positive")
	}

	lockKey := fmt.Sprintf("cordis:guild:quota:%s:%d", quota.Kind, quota.ScopeID)
	if quota.Kind == QuotaGuildChannels {
		lockKey = guildChannelMutationLockKey(quota.ScopeID)
	}
	if _, err := s.q.ExecContext(ctx, lockQuotaScopeStatement, lockKey); err != nil {
		return err
	}
	// Keep the count in a later statement: under READ COMMITTED it receives a
	// fresh snapshot after any previous holder of this scope lock commits.

	var count int
	var err error
	switch quota.Kind {
	case QuotaOwnedGuilds:
		err = s.q.QueryRowxContext(ctx, `
			SELECT COUNT(*) FROM guilds
			WHERE owner_id = $1 AND deleted_at = 0
		`, quota.ScopeID).Scan(&count)
	case QuotaJoinedGuilds:
		var exists bool
		err = s.q.QueryRowxContext(ctx, `
			SELECT
				EXISTS (
					SELECT 1 FROM guild_members
					WHERE user_id = $1 AND guild_id = $2 AND deleted_at = 0
				),
				(
					SELECT COUNT(*)
					FROM guild_members AS gm
					JOIN guilds AS g ON g.id = gm.guild_id AND g.deleted_at = 0
					WHERE gm.user_id = $1 AND gm.deleted_at = 0
				)
		`, quota.ScopeID, quota.TargetID).Scan(&exists, &count)
		if err == nil && exists {
			return ErrMemberAlreadyExists
		}
	case QuotaGuildRoles:
		err = s.q.QueryRowxContext(ctx, `
			SELECT COUNT(*) FROM roles
			WHERE guild_id = $1 AND deleted_at = 0
		`, quota.ScopeID).Scan(&count)
	case QuotaGuildChannels:
		err = s.q.QueryRowxContext(ctx, `
			SELECT COUNT(*) FROM guild_channels
			WHERE guild_id = $1 AND deleted_at = 0
		`, quota.ScopeID).Scan(&count)
	case QuotaActiveInvites:
		err = s.q.QueryRowxContext(ctx, `
			SELECT COUNT(*) FROM guild_invites
			WHERE guild_id = $1
			  AND (expires_at = 0 OR expires_at > $2)
			  AND (max_uses = 0 OR uses < max_uses)
		`, quota.ScopeID, quota.Now).Scan(&count)
	case QuotaChannelOverwrites:
		var exists bool
		err = s.q.QueryRowxContext(ctx, `
			SELECT
				EXISTS (
					SELECT 1 FROM guild_channel_permission_overwrites
					WHERE channel_id = $1 AND target_type = $2 AND target_id = $3
				),
				(SELECT COUNT(*) FROM guild_channel_permission_overwrites WHERE channel_id = $1)
		`, quota.ScopeID, quota.TargetType, quota.TargetID).Scan(&exists, &count)
		if err == nil && exists {
			return nil
		}
	default:
		return fmt.Errorf("unknown quota kind %q", quota.Kind)
	}
	if err != nil {
		return err
	}
	if count >= quota.Limit {
		return ErrResourceLimitExceeded
	}
	return nil
}

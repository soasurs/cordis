package store

import (
	"context"
	"strings"

	"github.com/jmoiron/sqlx"

	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

type guildMemberProfileRow struct {
	GuildID          int64  `db:"guild_id"`
	UserID           int64  `db:"user_id"`
	Username         string `db:"username"`
	Name             string `db:"name"`
	Nickname         string `db:"nickname"`
	UsernameSearch   string `db:"username_search"`
	NameSearch       string `db:"name_search"`
	NicknameSearch   string `db:"nickname_search"`
	AvatarAssetID    int64  `db:"avatar_asset_id"`
	ProfileUpdatedAt int64  `db:"profile_updated_at"`
}

func (s *SQLStore) SearchGuildMentionUsers(ctx context.Context, params SearchGuildMentionUsersParams) ([]*model.GuildMemberProfile, error) {
	pattern := escapeLikePrefix(params.Query) + "%"
	var rows []guildMemberProfileRow
	if err := sqlx.SelectContext(ctx, s.q, &rows, searchGuildMentionUsersQuery,
		params.GuildID,
		pattern,
		params.After,
		params.AfterMatchRank,
		params.AfterUsername,
		params.AfterUserID,
		params.Limit,
	); err != nil {
		return nil, err
	}
	profiles := make([]*model.GuildMemberProfile, 0, len(rows))
	for _, row := range rows {
		profiles = append(profiles, guildMemberProfileFromRow(&row))
	}
	return profiles, nil
}

func (s *SQLStore) UpsertGuildMemberProfile(ctx context.Context, profile *model.GuildMemberProfile) error {
	if profile == nil {
		return nil
	}
	_, err := s.q.ExecContext(ctx, upsertGuildMemberProfileQuery,
		profile.GuildID,
		profile.UserID,
		profile.Username,
		profile.Name,
		profile.Nickname,
		strings.ToLower(strings.TrimSpace(profile.Username)),
		strings.ToLower(strings.TrimSpace(profile.Name)),
		strings.ToLower(strings.TrimSpace(profile.Nickname)),
		profile.AvatarAssetID,
		profile.ProfileUpdatedAt,
	)
	return err
}

func (s *SQLStore) UpdateGuildMemberProfilesByUser(ctx context.Context, profile *model.GuildMemberProfile) error {
	if profile == nil {
		return nil
	}
	_, err := s.q.ExecContext(ctx, updateGuildMemberProfilesByUserQuery,
		profile.UserID,
		profile.Username,
		profile.Name,
		strings.ToLower(strings.TrimSpace(profile.Username)),
		strings.ToLower(strings.TrimSpace(profile.Name)),
		profile.AvatarAssetID,
		profile.ProfileUpdatedAt,
	)
	return err
}

func (s *SQLStore) UpdateGuildMemberProfilesByUserWithoutAvatar(ctx context.Context, profile *model.GuildMemberProfile) error {
	if profile == nil {
		return nil
	}
	_, err := s.q.ExecContext(ctx, updateGuildMemberProfilesByUserWithoutAvatarQuery,
		profile.UserID,
		profile.Username,
		profile.Name,
		strings.ToLower(strings.TrimSpace(profile.Username)),
		strings.ToLower(strings.TrimSpace(profile.Name)),
		profile.ProfileUpdatedAt,
	)
	return err
}

func (s *SQLStore) DeleteGuildMemberProfile(ctx context.Context, guildID, userID int64) error {
	_, err := s.q.ExecContext(ctx, deleteGuildMemberProfileQuery, guildID, userID)
	return err
}

func (s *SQLStore) UpdateGuildMemberProfileNickname(ctx context.Context, guildID, userID int64, nickname string) error {
	_, err := s.q.ExecContext(
		ctx,
		updateGuildMemberProfileNicknameQuery,
		guildID,
		userID,
		nickname,
		strings.ToLower(strings.TrimSpace(nickname)),
	)
	return err
}

func (s *SQLStore) DeleteGuildMemberProfiles(ctx context.Context, guildID int64) error {
	_, err := s.q.ExecContext(ctx, deleteGuildMemberProfilesStatement, guildID)
	return err
}

func (s *SQLStore) ListGuildMemberProfileKeys(ctx context.Context, params ListGuildMemberProfileKeysParams) ([]model.GuildMemberProfileKey, error) {
	var rows []model.GuildMemberProfileKey
	if err := sqlx.SelectContext(ctx, s.q, &rows, listGuildMemberProfileKeysQuery,
		params.AfterGuildID,
		params.AfterUserID,
		params.Limit,
	); err != nil {
		return nil, err
	}
	return rows, nil
}

func guildMemberProfileFromRow(row *guildMemberProfileRow) *model.GuildMemberProfile {
	return &model.GuildMemberProfile{
		GuildID:          row.GuildID,
		UserID:           row.UserID,
		Username:         row.Username,
		Name:             row.Name,
		Nickname:         row.Nickname,
		UsernameSearch:   row.UsernameSearch,
		NameSearch:       row.NameSearch,
		NicknameSearch:   row.NicknameSearch,
		AvatarAssetID:    row.AvatarAssetID,
		ProfileUpdatedAt: row.ProfileUpdatedAt,
	}
}

func escapeLikePrefix(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, "%", `\%`)
	return strings.ReplaceAll(value, "_", `\_`)
}

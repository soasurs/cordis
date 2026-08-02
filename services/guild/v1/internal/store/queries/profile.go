package queries

// SearchGuildMentionUsersQuery prefix-searches member profiles in a guild,
// ranking username matches first.
const SearchGuildMentionUsersQuery = `
WITH candidates AS (
    SELECT p.guild_id,
           p.user_id,
           p.username,
           p.name,
           p.nickname,
           p.username_search,
           p.name_search,
           p.nickname_search,
           p.avatar_asset_id,
           p.profile_updated_at,
           CASE WHEN p.username_search LIKE $2 ESCAPE '\' THEN 0 ELSE 1 END AS match_rank
    FROM guild_member_profiles AS p
    JOIN guild_members AS m
      ON m.guild_id = p.guild_id
     AND m.user_id = p.user_id
     AND m.deleted_at = 0
    WHERE p.guild_id = $1
      AND (
          p.nickname_search LIKE $2 ESCAPE '\'
          OR p.username_search LIKE $2 ESCAPE '\'
          OR p.name_search LIKE $2 ESCAPE '\'
      )
)
SELECT guild_id, user_id, username, name, nickname, username_search, name_search,
       nickname_search, avatar_asset_id, profile_updated_at
FROM candidates
WHERE NOT $3::BOOLEAN
   OR (match_rank, username_search, user_id) > ($4::INT, $5::TEXT, $6::BIGINT)
ORDER BY match_rank, username_search, user_id
LIMIT $7
`

// UpsertGuildMemberProfileQuery inserts or replaces a member profile when the
// update is newer.
const UpsertGuildMemberProfileQuery = `
INSERT INTO guild_member_profiles (
    guild_id, user_id, username, name, nickname, username_search, name_search,
    nickname_search, avatar_asset_id, profile_updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (guild_id, user_id) DO UPDATE
SET username = EXCLUDED.username,
    name = EXCLUDED.name,
    nickname = EXCLUDED.nickname,
    username_search = EXCLUDED.username_search,
    name_search = EXCLUDED.name_search,
    nickname_search = EXCLUDED.nickname_search,
    avatar_asset_id = EXCLUDED.avatar_asset_id,
    profile_updated_at = EXCLUDED.profile_updated_at
WHERE guild_member_profiles.profile_updated_at <= EXCLUDED.profile_updated_at
`

// UpdateGuildMemberProfilesByUserQuery refreshes a user's profiles across
// guilds when the update is newer, including the avatar.
const UpdateGuildMemberProfilesByUserQuery = `
UPDATE guild_member_profiles
SET username = $2,
    name = $3,
    username_search = $4,
    name_search = $5,
    avatar_asset_id = $6,
    profile_updated_at = $7
WHERE user_id = $1
  AND profile_updated_at <= $7
`

// UpdateGuildMemberProfilesByUserWithoutAvatarQuery refreshes a user's
// profiles across guilds when the update is newer, without touching avatars.
const UpdateGuildMemberProfilesByUserWithoutAvatarQuery = `
UPDATE guild_member_profiles
SET username = $2,
    name = $3,
    username_search = $4,
    name_search = $5,
    profile_updated_at = $6
WHERE user_id = $1
  AND profile_updated_at <= $6
`

// UpdateGuildMemberProfileNicknameQuery updates one guild profile's nickname
// and search text.
const UpdateGuildMemberProfileNicknameQuery = `
UPDATE guild_member_profiles
SET nickname = $3,
    nickname_search = $4
WHERE guild_id = $1
  AND user_id = $2
`

// ListGuildMemberProfileKeysQuery pages active member keys for profile
// hydration.
const ListGuildMemberProfileKeysQuery = `
SELECT guild_id, user_id, nickname
FROM guild_members
WHERE deleted_at = 0
  AND (
      $1::BIGINT = 0
      OR guild_id > $1::BIGINT
      OR (guild_id = $1::BIGINT AND user_id > $2::BIGINT)
  )
ORDER BY guild_id, user_id
LIMIT $3
`

// DeleteGuildMemberProfileQuery removes one guild member profile.
const DeleteGuildMemberProfileQuery = `
DELETE FROM guild_member_profiles
WHERE guild_id = $1
  AND user_id = $2
`

// DeleteGuildMemberProfilesStatement removes every profile in a guild.
const DeleteGuildMemberProfilesStatement = `
DELETE FROM guild_member_profiles
WHERE guild_id = $1
`

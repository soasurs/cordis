package server

import (
	"errors"
	"slices"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/soasurs/cordis/pkg/cursor"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
)

// userGuildsPayload continues ListUserGuilds for one user.
type userGuildsPayload struct {
	UserID int64 `json:"uid"`
	ID     int64 `json:"id"`
}

// guildIDPayload continues a guild-scoped ID-ordered list.
type guildIDPayload struct {
	GuildID int64 `json:"gid"`
	ID      int64 `json:"id"`
}

// guildTimeIDPayload continues a guild-scoped (time, id) list.
type guildTimeIDPayload struct {
	GuildID int64 `json:"gid"`
	Time    int64 `json:"t"`
	ID      int64 `json:"i"`
}

// guildRoleTimeIDPayload continues ListGuildRoleMembers for one role.
type guildRoleTimeIDPayload struct {
	GuildID int64 `json:"gid"`
	RoleID  int64 `json:"rid"`
	Time    int64 `json:"t"`
	ID      int64 `json:"i"`
}

// mentionTargetsPayload continues ListGuildMentionTargets and binds the
// pagination to the request parameters so a cursor cannot be reused with a
// different channel, role set, or everyone flag.
type mentionTargetsPayload struct {
	GuildID     int64   `json:"gid"`
	ChannelID   int64   `json:"cid"`
	RoleIDs     []int64 `json:"rids,omitempty"`
	Everyone    bool    `json:"all,omitempty"`
	AfterUserID int64   `json:"uid"`
}

func readCursor(has bool, value string) (string, error) {
	if !has {
		return "", nil
	}
	if value == "" {
		return "", invalidRequest("cursor is invalid")
	}
	return value, nil
}

func mapCursorErr(err error) error {
	if errors.Is(err, cursor.ErrInvalid) {
		return invalidRequest("cursor is invalid")
	}
	return err
}

func decodeUserGuildsCursor(c *cursor.Codec, token string, userID int64) (beforeID int64, ok bool, err error) {
	payload, ok, err := cursor.Decode[userGuildsPayload](c, cursor.KindUserGuilds, token)
	if err != nil {
		return 0, false, mapCursorErr(err)
	}
	if !ok {
		return 0, false, nil
	}
	if payload.UserID != userID || payload.ID <= 0 {
		return 0, false, invalidRequest("cursor is invalid")
	}
	return payload.ID, true, nil
}

func decodeGuildIDCursor(c *cursor.Codec, kind, token string, guildID int64) (beforeID int64, ok bool, err error) {
	payload, ok, err := cursor.Decode[guildIDPayload](c, kind, token)
	if err != nil {
		return 0, false, mapCursorErr(err)
	}
	if !ok {
		return 0, false, nil
	}
	if payload.GuildID != guildID || payload.ID <= 0 {
		return 0, false, invalidRequest("cursor is invalid")
	}
	return payload.ID, true, nil
}

func decodeGuildTimeIDCursor(c *cursor.Codec, kind, token string, guildID int64) (timeMs, id int64, ok bool, err error) {
	payload, ok, err := cursor.Decode[guildTimeIDPayload](c, kind, token)
	if err != nil {
		return 0, 0, false, mapCursorErr(err)
	}
	if !ok {
		return 0, 0, false, nil
	}
	if payload.GuildID != guildID || payload.Time <= 0 || payload.ID <= 0 {
		return 0, 0, false, invalidRequest("cursor is invalid")
	}
	return payload.Time, payload.ID, true, nil
}

func decodeMentionTargetsCursor(
	c *cursor.Codec,
	token string,
	guildID, channelID int64,
	roleIDs []int64,
	everyone bool,
) (afterUserID int64, ok bool, err error) {
	payload, ok, err := cursor.Decode[mentionTargetsPayload](c, cursor.KindGuildMentionTargets, token)
	if err != nil {
		return 0, false, mapCursorErr(err)
	}
	if !ok {
		return 0, false, nil
	}
	if payload.GuildID != guildID ||
		payload.ChannelID != channelID ||
		payload.AfterUserID <= 0 ||
		payload.Everyone != everyone ||
		!slices.Equal(payload.RoleIDs, roleIDs) {
		return 0, false, invalidRequest("cursor is invalid")
	}
	return payload.AfterUserID, true, nil
}

func setNextMentionTargetsCursor(
	c *cursor.Codec,
	set func(string),
	hasMore bool,
	afterUserID, guildID, channelID int64,
	roleIDs []int64,
	everyone bool,
) error {
	if !hasMore || afterUserID <= 0 {
		return nil
	}
	token, err := c.Encode(cursor.KindGuildMentionTargets, mentionTargetsPayload{
		GuildID:     guildID,
		ChannelID:   channelID,
		RoleIDs:     roleIDs,
		Everyone:    everyone,
		AfterUserID: afterUserID,
	})
	if err != nil {
		return status.Error(codes.Internal, "failed to encode cursor")
	}
	set(token)
	return nil
}

func decodeGuildRoleTimeIDCursor(c *cursor.Codec, token string, guildID, roleID int64) (timeMs, id int64, ok bool, err error) {
	payload, ok, err := cursor.Decode[guildRoleTimeIDPayload](c, cursor.KindGuildRoleMembers, token)
	if err != nil {
		return 0, 0, false, mapCursorErr(err)
	}
	if !ok {
		return 0, 0, false, nil
	}
	if payload.GuildID != guildID || payload.RoleID != roleID || payload.Time <= 0 || payload.ID <= 0 {
		return 0, 0, false, invalidRequest("cursor is invalid")
	}
	return payload.Time, payload.ID, true, nil
}

func setNextUserGuildsCursor(c *cursor.Codec, set func(string), hasMore bool, page []*model.Guild, userID int64) error {
	if !hasMore || len(page) == 0 {
		return nil
	}
	id := page[len(page)-1].ID
	if userID <= 0 || id <= 0 {
		return status.Error(codes.Internal, "failed to encode cursor")
	}
	token, err := c.Encode(cursor.KindUserGuilds, userGuildsPayload{UserID: userID, ID: id})
	if err != nil {
		return status.Error(codes.Internal, "failed to encode cursor")
	}
	set(token)
	return nil
}

func setNextGuildIDCursor[T any](c *cursor.Codec, kind string, set func(string), hasMore bool, page []T, guildID int64, idOf func(T) int64) error {
	if !hasMore || len(page) == 0 {
		return nil
	}
	id := idOf(page[len(page)-1])
	if guildID <= 0 || id <= 0 {
		return status.Error(codes.Internal, "failed to encode cursor")
	}
	token, err := c.Encode(kind, guildIDPayload{GuildID: guildID, ID: id})
	if err != nil {
		return status.Error(codes.Internal, "failed to encode cursor")
	}
	set(token)
	return nil
}

func setNextMemberCursor(c *cursor.Codec, kind string, set func(string), hasMore bool, page []*model.GuildMember, guildID int64) error {
	if !hasMore || len(page) == 0 {
		return nil
	}
	last := page[len(page)-1]
	if guildID <= 0 || last.JoinedAt <= 0 || last.UserID <= 0 {
		return status.Error(codes.Internal, "failed to encode cursor")
	}
	token, err := c.Encode(kind, guildTimeIDPayload{GuildID: guildID, Time: last.JoinedAt, ID: last.UserID})
	if err != nil {
		return status.Error(codes.Internal, "failed to encode cursor")
	}
	set(token)
	return nil
}

func setNextRoleMemberCursor(c *cursor.Codec, set func(string), hasMore bool, page []*model.GuildMember, guildID, roleID int64) error {
	if !hasMore || len(page) == 0 {
		return nil
	}
	last := page[len(page)-1]
	if guildID <= 0 || roleID <= 0 || last.JoinedAt <= 0 || last.UserID <= 0 {
		return status.Error(codes.Internal, "failed to encode cursor")
	}
	token, err := c.Encode(cursor.KindGuildRoleMembers, guildRoleTimeIDPayload{
		GuildID: guildID, RoleID: roleID, Time: last.JoinedAt, ID: last.UserID,
	})
	if err != nil {
		return status.Error(codes.Internal, "failed to encode cursor")
	}
	set(token)
	return nil
}

func setNextBanCursor(c *cursor.Codec, set func(string), hasMore bool, page []*model.GuildBan, guildID int64) error {
	if !hasMore || len(page) == 0 {
		return nil
	}
	last := page[len(page)-1]
	if guildID <= 0 || last.CreatedAt <= 0 || last.UserID <= 0 {
		return status.Error(codes.Internal, "failed to encode cursor")
	}
	token, err := c.Encode(cursor.KindGuildBans, guildTimeIDPayload{GuildID: guildID, Time: last.CreatedAt, ID: last.UserID})
	if err != nil {
		return status.Error(codes.Internal, "failed to encode cursor")
	}
	set(token)
	return nil
}

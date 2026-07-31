package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

const (
	createGuildOperation        = "guild.create"
	createGuildRoleOperation    = "guild.role.create"
	createGuildChannelOperation = "guild.channel.create"
	createGuildInviteOperation  = "guild.invite.create"
)

type guildFingerprint struct {
	Version int `json:"version"`
}

func fingerprintHash(value any) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal fingerprint: %w", err)
	}
	digest := sha256.Sum256(data)
	return digest[:], nil
}

func createGuildRequestHash(name string) ([]byte, error) {
	return fingerprintHash(struct {
		guildFingerprint
		Name string `json:"name"`
	}{guildFingerprint{Version: 1}, name})
}

func createGuildRoleRequestHash(guildID int64, name string, permissions uint64) ([]byte, error) {
	return fingerprintHash(struct {
		guildFingerprint
		GuildID     int64  `json:"guild_id"`
		Name        string `json:"name"`
		Permissions uint64 `json:"permissions"`
	}{guildFingerprint{Version: 1}, guildID, name, permissions})
}

func createGuildChannelRequestHash(guildID int64, name string, channelType int32, topic string, parentID int64) ([]byte, error) {
	return fingerprintHash(struct {
		guildFingerprint
		GuildID  int64  `json:"guild_id"`
		Name     string `json:"name"`
		Type     int32  `json:"type"`
		Topic    string `json:"topic"`
		ParentID int64  `json:"parent_id"`
	}{guildFingerprint{Version: 1}, guildID, name, channelType, topic, parentID})
}

func createGuildInviteRequestHash(guildID int64, maxUses int32, expiresInMs int64) ([]byte, error) {
	// expires_in_ms is fingerprinted in its original relative form because the
	// absolute expiration recomputed for a retry would never match the first
	// attempt.
	return fingerprintHash(struct {
		guildFingerprint
		GuildID     int64 `json:"guild_id"`
		MaxUses     int32 `json:"max_uses"`
		ExpiresInMs int64 `json:"expires_in_ms"`
	}{guildFingerprint{Version: 1}, guildID, maxUses, expiresInMs})
}

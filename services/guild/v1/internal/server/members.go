package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	guildv1 "github.com/soasurs/cordis/gen/guild/v1"
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/guild/v1/internal/model"
	"github.com/soasurs/cordis/services/guild/v1/internal/store"
)

func (s *guildServer) AddGuildMember(ctx context.Context, req *guildv1.AddGuildMemberRequest) (*guildv1.AddGuildMemberResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}

	authority, err := loadMemberAuthority(ctx, s.svcCtx.Store, req.GetGuildId(), req.GetActorUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	if !authority.has(PermissionManageMembers) {
		return nil, permissionDenied()
	}

	userReq := new(userv1.GetUserRequest)
	userReq.SetUserId(req.GetUserId())
	userResp, err := s.svcCtx.UserClient.GetUser(ctx, userReq)
	if err != nil {
		return nil, err
	}
	if userResp.GetUser().GetUserId() != req.GetUserId() {
		return nil, notFound()
	}

	var member *model.GuildMember
	joinedAt := time.Now().UnixMilli()
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		current, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !current.has(PermissionManageMembers) {
			return permissionDenied()
		}
		if _, err := txStore.GetGuildBan(ctx, req.GetGuildId(), req.GetUserId()); err == nil {
			return store.ErrUserBanned
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		member, err = txStore.CreateGuildMember(ctx, req.GetGuildId(), req.GetUserId(), joinedAt)
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}

	event, eventErr := newGuildMemberJoinedEvent(member)
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.AddGuildMemberResponse)
	resp.SetMember(guildMemberToProto(member))
	return resp, nil
}

func (s *guildServer) GetGuildMember(ctx context.Context, req *guildv1.GetGuildMemberRequest) (*guildv1.GetGuildMemberResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	if _, err := s.svcCtx.Store.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, mapStoreError(err)
	}
	member, err := s.svcCtx.Store.GetGuildMember(ctx, req.GetGuildId(), req.GetUserId())
	if err != nil {
		return nil, mapStoreError(err)
	}
	resp := new(guildv1.GetGuildMemberResponse)
	resp.SetMember(guildMemberToProto(member))
	return resp, nil
}

func (s *guildServer) ListGuildMembers(ctx context.Context, req *guildv1.ListGuildMembersRequest) (*guildv1.ListGuildMembersResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetBeforeUserId() < 0 {
		return nil, invalidRequest("before user id must not be negative")
	}
	limit, err := normalizeLimit(req.GetLimit())
	if err != nil {
		return nil, err
	}
	if _, err := s.svcCtx.Store.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, mapStoreError(err)
	}
	members, err := s.svcCtx.Store.ListGuildMembers(ctx, store.ListGuildMembersParams{
		GuildID: req.GetGuildId(), BeforeUserID: req.GetBeforeUserId(), Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	resp := new(guildv1.ListGuildMembersResponse)
	resp.SetMembers(guildMembersToProto(members))
	if len(members) > 0 {
		resp.SetBeforeUserId(members[len(members)-1].UserID)
	}
	return resp, nil
}

func (s *guildServer) UpdateGuildMember(ctx context.Context, req *guildv1.UpdateGuildMemberRequest) (*guildv1.UpdateGuildMemberResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	nickname, err := normalizeNickname(req.GetNickname())
	if err != nil {
		return nil, err
	}
	var member *model.GuildMember
	err = s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		if _, err := txStore.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId()); err != nil {
			return err
		}
		member, err = txStore.UpdateGuildMemberNickname(ctx, req.GetGuildId(), req.GetActorUserId(), nickname)
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildMemberUpdatedEvent(member)
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.UpdateGuildMemberResponse)
	resp.SetMember(guildMemberToProto(member))
	return resp, nil
}

func (s *guildServer) KickGuildMember(ctx context.Context, req *guildv1.KickGuildMemberRequest) (*guildv1.KickGuildMemberResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	var removed *model.GuildMember
	removedAt := time.Now().UnixMilli()
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		actor, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if !actor.has(PermissionKickMembers) {
			return permissionDenied()
		}
		target, err := loadMemberAuthority(ctx, txStore, req.GetGuildId(), req.GetUserId())
		if err != nil {
			return err
		}
		if target.IsOwner {
			return invalidRequest("guild owner cannot be kicked")
		}
		if !canManageMember(actor, target) {
			return permissionDenied()
		}
		if err := txStore.DeleteGuildMemberRoleAssignments(ctx, req.GetGuildId(), req.GetUserId()); err != nil {
			return err
		}
		if err := txStore.DeleteGuildChannelPermissionOverwritesForTarget(
			ctx,
			req.GetGuildId(),
			int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER),
			req.GetUserId(),
		); err != nil {
			return err
		}
		removed, err = txStore.RemoveGuildMember(ctx, req.GetGuildId(), req.GetUserId(), removedAt)
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildMemberRemovedEvent(removed)
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.KickGuildMemberResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *guildServer) LeaveGuild(ctx context.Context, req *guildv1.LeaveGuildRequest) (*guildv1.LeaveGuildResponse, error) {
	if req.GetGuildId() <= 0 {
		return nil, invalidRequest("guild id is required")
	}
	if req.GetUserId() <= 0 {
		return nil, invalidRequest("user id is required")
	}
	var removed *model.GuildMember
	removedAt := time.Now().UnixMilli()
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		guild, err := txStore.GetGuildForMember(ctx, req.GetGuildId(), req.GetUserId())
		if err != nil {
			return err
		}
		if guild.OwnerID == req.GetUserId() {
			return invalidRequest("guild owner must transfer ownership before leaving")
		}
		if err := txStore.DeleteGuildMemberRoleAssignments(ctx, req.GetGuildId(), req.GetUserId()); err != nil {
			return err
		}
		if err := txStore.DeleteGuildChannelPermissionOverwritesForTarget(
			ctx,
			req.GetGuildId(),
			int32(guildv1.GuildPermissionOverwriteType_GUILD_PERMISSION_OVERWRITE_TYPE_MEMBER),
			req.GetUserId(),
		); err != nil {
			return err
		}
		removed, err = txStore.RemoveGuildMember(ctx, req.GetGuildId(), req.GetUserId(), removedAt)
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildMemberRemovedEvent(removed)
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.LeaveGuildResponse)
	resp.SetOk(true)
	return resp, nil
}

func (s *guildServer) TransferGuildOwnership(ctx context.Context, req *guildv1.TransferGuildOwnershipRequest) (*guildv1.TransferGuildOwnershipResponse, error) {
	if err := validateMemberActorRequest(req.GetGuildId(), req.GetActorUserId()); err != nil {
		return nil, err
	}
	if req.GetNewOwnerId() <= 0 {
		return nil, invalidRequest("new owner id is required")
	}
	if req.GetNewOwnerId() == req.GetActorUserId() {
		return nil, invalidRequest("new owner must differ from current owner")
	}
	var updated *model.Guild
	err := s.svcCtx.Store.Transact(ctx, func(txStore store.Store) error {
		guild, err := txStore.GetGuildForMember(ctx, req.GetGuildId(), req.GetActorUserId())
		if err != nil {
			return err
		}
		if guild.OwnerID != req.GetActorUserId() {
			return permissionDenied()
		}
		if _, err := txStore.GetGuildMember(ctx, req.GetGuildId(), req.GetNewOwnerId()); err != nil {
			return err
		}
		updated, err = txStore.TransferGuildOwnership(ctx, req.GetGuildId(), req.GetActorUserId(), req.GetNewOwnerId())
		return err
	})
	if err != nil {
		return nil, mapStoreError(err)
	}
	event, eventErr := newGuildUpdatedEvent(updated)
	s.publishEvent(ctx, event, eventErr)
	resp := new(guildv1.TransferGuildOwnershipResponse)
	resp.SetGuild(guildToProto(updated))
	return resp, nil
}

func validateMemberActorRequest(guildID, actorUserID int64) error {
	if guildID <= 0 {
		return invalidRequest("guild id is required")
	}
	if actorUserID <= 0 {
		return invalidRequest("actor user id is required")
	}
	return nil
}

func guildMemberToProto(member *model.GuildMember) *guildv1.GuildMember {
	if member == nil {
		return nil
	}
	value := new(guildv1.GuildMember)
	value.SetGuildId(member.GuildID)
	value.SetUserId(member.UserID)
	value.SetNickname(member.Nickname)
	value.SetRevision(member.Revision)
	value.SetJoinedAt(member.JoinedAt)
	value.SetUpdatedAt(member.UpdatedAt)
	return value
}

func guildMembersToProto(members []*model.GuildMember) []*guildv1.GuildMember {
	values := make([]*guildv1.GuildMember, 0, len(members))
	for _, member := range members {
		values = append(values, guildMemberToProto(member))
	}
	return values
}

package server

import (
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

func userToProto(user *model.User) *userv1.User {
	pbUser := new(userv1.User)
	pbUser.SetUserId(user.UserID)
	pbUser.SetEmail(user.Email)
	pbUser.SetCreatedAt(user.CreatedAt)
	pbUser.SetUpdatedAt(user.UpdatedAt)
	pbUser.SetDeletedAt(user.DeletedAt)
	pbUser.SetEmailVerifiedAt(user.EmailVerifiedAt)
	return pbUser
}

func userProfileToProto(profile *model.UserProfile) *userv1.UserProfile {
	pbProfile := new(userv1.UserProfile)
	pbProfile.SetUserId(profile.UserID)
	pbProfile.SetUsername(profile.Username)
	pbProfile.SetName(profile.Name)
	pbProfile.SetAvatarUri(profile.AvatarURI)
	pbProfile.SetCreatedAt(profile.CreatedAt)
	pbProfile.SetUpdatedAt(profile.UpdatedAt)
	pbProfile.SetDeletedAt(profile.DeletedAt)
	return pbProfile
}

func relationshipToProto(relationship *model.Relationship) *userv1.Relationship {
	if relationship == nil {
		return nil
	}
	value := new(userv1.Relationship)
	value.SetUserId(relationship.UserID)
	value.SetTargetId(relationship.TargetID)
	value.SetType(userv1.RelationshipType(relationship.Type))
	value.SetCreatedAt(relationship.CreatedAt)
	value.SetUpdatedAt(relationship.UpdatedAt)
	return value
}

func relationshipsToProto(relationships []*model.Relationship) []*userv1.Relationship {
	values := make([]*userv1.Relationship, 0, len(relationships))
	for _, relationship := range relationships {
		values = append(values, relationshipToProto(relationship))
	}
	return values
}

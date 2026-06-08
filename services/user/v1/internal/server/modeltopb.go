package server

import (
	userv1 "github.com/soasurs/cordis/gen/user/v1"
	"github.com/soasurs/cordis/services/user/v1/internal/model"
)

func toPBUser(user *model.User) *userv1.User {
	pbUser := new(userv1.User)
	pbUser.SetUserId(user.UserID)
	pbUser.SetEmail(user.Email)
	pbUser.SetCreatedAt(user.CreatedAt)
	pbUser.SetUpdatedAt(user.UpdatedAt)
	pbUser.SetDeletedAt(user.DeletedAt)
	return pbUser
}

func toPBUserProfile(profile *model.UserProfile) *userv1.UserProfile {
	pbProfile := new(userv1.UserProfile)
	pbProfile.SetUserId(profile.UserID)
	pbProfile.SetName(profile.Name)
	pbProfile.SetAvatarUri(profile.AvatarURI)
	pbProfile.SetCreatedAt(profile.CreatedAt)
	pbProfile.SetUpdatedAt(profile.UpdatedAt)
	pbProfile.SetDeletedAt(profile.DeletedAt)
	return pbProfile
}

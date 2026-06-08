//go:build integration

package store

import (
	"context"
	"testing"

	"github.com/soasurs/cordis/internal/testpostgres"
	usermigrations "github.com/soasurs/cordis/services/user/v1/db/migrations"
)

func TestSQLStoreUserLifecycle(t *testing.T) {
	ctx := context.Background()
	store := New(testpostgres.New(t, usermigrations.Files))

	err := store.Transact(ctx, func(txStore Store) error {
		if _, err := txStore.CreateUser(ctx, 1001, "user@example.com", "hashed-password"); err != nil {
			return err
		}
		_, err := txStore.CreateUserProfile(ctx, 1001, "display name", "avatar://1")
		return err
	})
	if err != nil {
		t.Fatalf("create user transaction: %v", err)
	}

	user, err := store.GetUser(ctx, 1001)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if user.Email != "user@example.com" || user.HashedPassword != "hashed-password" {
		t.Fatalf("unexpected user: %+v", user)
	}

	userWithEmail, err := store.GetUserWithEmail(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("get user with email: %v", err)
	}
	if userWithEmail.UserID != 1001 {
		t.Fatalf("unexpected user: %+v", userWithEmail)
	}

	profile, err := store.GetUserProfile(ctx, 1001)
	if err != nil {
		t.Fatalf("get user profile: %v", err)
	}
	if profile.Name != "display name" || profile.AvatarURI != "avatar://1" {
		t.Fatalf("unexpected profile: %+v", profile)
	}

	available, err := store.CheckEmailAvailability(ctx, "user@example.com")
	if err != nil {
		t.Fatalf("check existing email: %v", err)
	}
	if available {
		t.Fatal("existing email should not be available")
	}

	available, err = store.CheckEmailAvailability(ctx, "available@example.com")
	if err != nil {
		t.Fatalf("check available email: %v", err)
	}
	if !available {
		t.Fatal("unused email should be available")
	}

	if err := store.UpdateUserPassword(ctx, 1001, "new-hashed-password"); err != nil {
		t.Fatalf("update password: %v", err)
	}

	updatedUser, err := store.UpdateUserEmail(ctx, 1001, "new@example.com")
	if err != nil {
		t.Fatalf("update email: %v", err)
	}
	if updatedUser.Email != "new@example.com" {
		t.Fatalf("unexpected updated user: %+v", updatedUser)
	}

	reloadedUser, err := store.GetUser(ctx, 1001)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloadedUser.HashedPassword != "new-hashed-password" || reloadedUser.Email != "new@example.com" {
		t.Fatalf("unexpected reloaded user: %+v", reloadedUser)
	}
}

func TestSQLStoreEnforcesActiveEmailUniqueness(t *testing.T) {
	ctx := context.Background()
	store := New(testpostgres.New(t, usermigrations.Files))

	if _, err := store.CreateUser(ctx, 1001, "user@example.com", "hashed-password"); err != nil {
		t.Fatalf("create first user: %v", err)
	}
	if _, err := store.CreateUser(ctx, 1002, "user@example.com", "hashed-password"); err == nil {
		t.Fatal("expected duplicate email error")
	}
}

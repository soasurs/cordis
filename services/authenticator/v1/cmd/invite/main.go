package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/conf"

	"github.com/soasurs/cordis/pkg/database"
	"github.com/soasurs/cordis/pkg/snowflake"
	"github.com/soasurs/cordis/services/authenticator/v1/config"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/token"
)

const defaultListLimit = 50

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	global := flag.NewFlagSet("invite", flag.ContinueOnError)
	configPath := global.String("c", "etc/config.yaml", "authenticator config file")
	global.SetOutput(os.Stderr)
	if err := global.Parse(args); err != nil {
		return err
	}
	if global.NArg() == 0 {
		return errors.New("usage: invite [-c config] <create|list|revoke> [options]")
	}

	cfg := new(config.Config)
	if err := conf.LoadConfig(*configPath, cfg, conf.UseEnv()); err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	db, err := database.NewPostgres(cfg.Database)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer db.Close()

	inviteStore := store.New(db)
	commandArgs := global.Args()[1:]
	switch global.Arg(0) {
	case "create":
		return createInvites(ctx, inviteStore, commandArgs)
	case "list":
		return listInvites(ctx, inviteStore, commandArgs)
	case "revoke":
		return revokeInvite(ctx, inviteStore, commandArgs)
	default:
		return fmt.Errorf("unknown command %q", global.Arg(0))
	}
}

func createInvites(ctx context.Context, inviteStore store.Store, args []string) error {
	flags := flag.NewFlagSet("create", flag.ContinueOnError)
	count := flags.Int("count", 1, "number of one-time invites to create")
	ttl := flags.Duration("ttl", 7*24*time.Hour, "invite lifetime; zero never expires")
	email := flags.String("email", "", "optional email address to bind")
	label := flags.String("label", "", "optional administrative label")
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("create accepts flags only")
	}
	if *count <= 0 || *count > 1000 {
		return errors.New("count must be between 1 and 1000")
	}
	if *ttl < 0 {
		return errors.New("ttl must not be negative")
	}

	boundEmail, err := normalizeBoundEmail(*email)
	if err != nil {
		return err
	}
	if boundEmail != "" && *count != 1 {
		return errors.New("email-bound invite creation requires count=1")
	}

	node, err := snowflake.New()
	if err != nil {
		return fmt.Errorf("create snowflake node: %w", err)
	}
	for range *count {
		rawCode, err := token.GenerateOpaqueToken()
		if err != nil {
			return err
		}
		now := time.Now()
		var expiresAt int64
		if *ttl > 0 {
			expiresAt = now.Add(*ttl).UnixMilli()
		}
		invite := &model.RegistrationInvite{
			ID:         node.Generate().Int64(),
			CodeHash:   token.Hash(rawCode),
			BoundEmail: boundEmail,
			ExpiresAt:  expiresAt,
			Label:      strings.TrimSpace(*label),
			CreatedAt:  now.UnixMilli(),
		}
		if err := inviteStore.CreateRegistrationInvite(ctx, invite); err != nil {
			return fmt.Errorf("create invite: %w", err)
		}
		fmt.Printf("id=%d code=%s email=%s expires_at=%d\n", invite.ID, rawCode, invite.BoundEmail, invite.ExpiresAt)
	}
	return nil
}

func listInvites(ctx context.Context, inviteStore store.Store, args []string) error {
	flags := flag.NewFlagSet("list", flag.ContinueOnError)
	beforeID := flags.Int64("before", 0, "return invites with IDs below this cursor")
	limit := flags.Int("limit", defaultListLimit, "maximum number of invites")
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("list accepts flags only")
	}
	if *beforeID < 0 {
		return errors.New("before must not be negative")
	}
	if *limit <= 0 || *limit > 1000 {
		return errors.New("limit must be between 1 and 1000")
	}

	invites, err := inviteStore.ListRegistrationInvites(ctx, *beforeID, *limit)
	if err != nil {
		return fmt.Errorf("list invites: %w", err)
	}
	now := time.Now().UnixMilli()
	for _, invite := range invites {
		fmt.Printf(
			"id=%d status=%s email=%s label=%q expires_at=%d redeemed_user_id=%d\n",
			invite.ID,
			inviteStatus(invite, now),
			invite.BoundEmail,
			invite.Label,
			invite.ExpiresAt,
			invite.RedeemedUserID,
		)
	}
	return nil
}

func revokeInvite(ctx context.Context, inviteStore store.Store, args []string) error {
	flags := flag.NewFlagSet("revoke", flag.ContinueOnError)
	id := flags.Int64("id", 0, "invite ID")
	flags.SetOutput(os.Stderr)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("revoke accepts flags only")
	}
	if *id <= 0 {
		return errors.New("id must be positive")
	}
	if err := inviteStore.RevokeRegistrationInvite(ctx, *id, time.Now().UnixMilli()); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("invite not found or no longer revocable")
		}
		return fmt.Errorf("revoke invite: %w", err)
	}
	fmt.Println("revoked invite " + strconv.FormatInt(*id, 10))
	return nil
}

func normalizeBoundEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" {
		return "", nil
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", errors.New("email is invalid")
	}
	return email, nil
}

func inviteStatus(invite *model.RegistrationInvite, now int64) string {
	switch {
	case invite.RedeemedAt != 0:
		return "redeemed"
	case invite.RevokedAt != 0:
		return "revoked"
	case invite.ExpiresAt != 0 && invite.ExpiresAt <= now:
		return "expired"
	case invite.ReservedUntil > now:
		return "reserved"
	default:
		return "available"
	}
}

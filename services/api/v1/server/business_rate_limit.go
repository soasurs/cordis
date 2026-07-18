package server

import (
	"context"
	"errors"
	"strconv"

	"connectrpc.com/connect"
	"github.com/zeromicro/go-zero/core/logx"

	apiratelimit "github.com/soasurs/cordis/services/api/v1/ratelimit"
	"github.com/soasurs/cordis/services/api/v1/svc"
)

func checkUserPolicy(ctx context.Context, policy string, userID int64) error {
	return apiratelimit.CheckKey(ctx, policy, strconv.FormatInt(userID, 10))
}

func checkResourcePolicy(ctx context.Context, policy string, resourceID int64) error {
	return apiratelimit.CheckKey(ctx, policy, strconv.FormatInt(resourceID, 10))
}

func checkUserPairPolicy(ctx context.Context, policy string, userID, targetID int64) error {
	key := strconv.FormatInt(userID, 10) + ":" + strconv.FormatInt(targetID, 10)
	return apiratelimit.CheckKey(ctx, policy, key)
}

func checkRelationshipWrite(ctx context.Context, userID int64) error {
	return checkUserPolicy(ctx, apiratelimit.PolicyRelationshipWrite, userID)
}

func checkGuildResourceCreate(ctx context.Context, actorUserID, guildID int64) error {
	if err := checkUserPolicy(ctx, apiratelimit.PolicyGuildResourceCreateActor, actorUserID); err != nil {
		return err
	}
	return checkResourcePolicy(ctx, apiratelimit.PolicyGuildResourceCreateGuild, guildID)
}

func acquireUserConcurrency(
	ctx context.Context,
	limiter svc.KeyedConcurrencyLimiter,
	userID int64,
) (func(), error) {
	if limiter == nil {
		return func() {}, nil
	}
	release, err := limiter.Acquire(ctx, strconv.FormatInt(userID, 10), 1)
	if err == nil {
		return release, nil
	}
	if errors.Is(err, context.Canceled) {
		return nil, connect.NewError(connect.CodeCanceled, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return nil, connect.NewError(connect.CodeDeadlineExceeded, err)
	}
	logx.WithContext(ctx).Errorw("acquire user concurrency", logx.Field("error", err))
	return nil, connect.NewError(connect.CodeInternal, errors.New("concurrency limiter unavailable"))
}

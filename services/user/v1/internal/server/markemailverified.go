package server

import (
	"context"
	"strings"
	"time"

	userv1 "github.com/soasurs/cordis/gen/user/v1"
)

func (s *userServer) MarkEmailVerified(ctx context.Context, req *userv1.MarkEmailVerifiedRequest) (*userv1.MarkEmailVerifiedResponse, error) {
	if req.GetUserId() <= 0 {
		return nil, errUserIDRequired
	}
	if strings.TrimSpace(req.GetEmail()) == "" {
		return nil, errEmailRequired
	}

	verifiedAt := req.GetVerifiedAt()
	if verifiedAt <= 0 {
		verifiedAt = time.Now().UnixMilli()
	}
	// The email predicate keeps stale verification tokens from confirming an
	// address the user has since replaced.
	if err := s.svcCtx.Store.MarkUserEmailVerified(ctx, req.GetUserId(), req.GetEmail(), verifiedAt); err != nil {
		return nil, mapStoreError(err)
	}

	resp := new(userv1.MarkEmailVerifiedResponse)
	resp.SetOk(true)
	return resp, nil
}

package server

import (
	"context"
	"errors"
	mathrand "math/rand/v2"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	presencev1 "github.com/soasurs/cordis/gen/presence/v1"
	"github.com/soasurs/cordis/pkg/observability"
	"github.com/soasurs/cordis/services/session/v1/internal/store"
)

func (s *Server) refreshSessionLeaseLoop(ctx context.Context) {
	nextCycle := time.Now().Add(jitterDuration(s.svcCtx.Cfg.Node.SessionLeaseRefreshInterval()))
	for {
		if !waitUntil(ctx, nextCycle) {
			return
		}
		cycleStart := time.Now()
		nextCycle = cycleStart.Add(jitterDuration(s.svcCtx.Cfg.Node.SessionLeaseRefreshInterval()))
		s.refreshSessionLeases(ctx)
	}
}

func jitterDuration(base time.Duration) time.Duration {
	if base <= 0 {
		return time.Millisecond
	}
	factor := 1 - leaseJitter + mathrand.Float64()*(2*leaseJitter)
	return time.Duration(float64(base) * factor)
}

func waitUntil(ctx context.Context, target time.Time) bool {
	delay := time.Until(target)
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func batchRefreshOffset(batch, batchCount int, spread time.Duration) time.Duration {
	if batchCount <= 1 || spread <= 0 {
		return 0
	}
	slot := spread / time.Duration(batchCount)
	if slot <= 0 {
		return 0
	}
	return time.Duration(batch)*slot + time.Duration(mathrand.Int64N(int64(slot)))
}

func (s *Server) refreshSessionLeases(ctx context.Context) {
	s.refreshSessionLeasesWithSpread(ctx, s.svcCtx.Cfg.Node.SessionLeaseSpreadWindow())
}

func (s *Server) refreshSessionLeasesWithSpread(ctx context.Context, spread time.Duration) {
	tracer := s.tracer
	if tracer == nil {
		tracer = otel.Tracer(observability.SessionInstrumentationName)
	}
	ctx, span := tracer.Start(ctx, "session.lease.refresh", trace.WithSpanKind(trace.SpanKindInternal))
	outcome := leaseRefreshOutcome{}
	defer func() {
		result := "success"
		if ctx.Err() != nil {
			result = "canceled"
		} else if outcome.ownerFailures > 0 || outcome.presenceFailures > 0 {
			result = "partial_failure"
		}
		span.SetAttributes(
			attribute.Int("cordis.session.lease.completed_batch_count", outcome.completedBatchCount),
			attribute.Int("cordis.session.lease.owner_failure_count", outcome.ownerFailures),
			attribute.Int("cordis.session.lease.presence_failure_count", outcome.presenceFailures),
			attribute.String("cordis.session.lease.owner_error_type", defaultLeaseErrorType(outcome.ownerErrorType)),
			attribute.String("cordis.session.lease.presence_error_type", defaultLeaseErrorType(outcome.presenceErrorType)),
			attribute.String("cordis.session.lease.result", result),
		)
		if result != "success" {
			span.SetStatus(otelcodes.Error, result)
		}
		span.End()
	}()

	s.mu.RLock()
	sessions := make([]*logicalSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, session)
	}
	s.mu.RUnlock()
	batchCount := (len(sessions) + leaseBatchSize - 1) / leaseBatchSize
	span.SetAttributes(
		attribute.Int("cordis.session.lease.session_count", len(sessions)),
		attribute.Int("cordis.session.lease.batch_count", batchCount),
		attribute.Int("cordis.session.lease.batch_size", leaseBatchSize),
		attribute.Int64("cordis.session.lease.interval_ms", s.svcCtx.Cfg.Node.SessionLeaseRefreshInterval().Milliseconds()),
		attribute.Int64("cordis.session.lease.spread_ms", spread.Milliseconds()),
	)
	cycleStart := time.Now()
	for batch, start := 0, 0; start < len(sessions); batch, start = batch+1, start+leaseBatchSize {
		if !waitUntil(ctx, cycleStart.Add(batchRefreshOffset(batch, batchCount, spread))) {
			return
		}
		end := min(start+leaseBatchSize, len(sessions))
		outcome.merge(s.refreshSessionLeaseBatch(ctx, sessions[start:end]))
		outcome.completedBatchCount++
	}
}

func (s *Server) refreshSessionLeaseBatch(ctx context.Context, sessions []*logicalSession) leaseRefreshOutcome {
	outcome := leaseRefreshOutcome{}
	owners := make([]store.Owner, 0, len(sessions))
	for _, session := range sessions {
		owners = append(owners, store.Owner{SessionID: session.id, NodeID: s.nodeID, Generation: s.generation})
	}
	if err := s.svcCtx.Store.SetOwners(ctx, owners, s.svcCtx.Cfg.Node.ResumeTTL()); err != nil {
		outcome.recordOwnerFailure(err)
		if ctx.Err() == nil {
			logx.WithContext(ctx).Errorw("refresh session owners", logx.Field("count", len(owners)), logx.Field("error", err))
		}
	}
	req := new(presencev1.RefreshUserSessionsRequest)
	items := make([]*presencev1.RefreshUserSessionRequest, 0, len(sessions))
	byID := make(map[string]*logicalSession, len(sessions))
	for _, session := range sessions {
		items = append(items, presenceRefreshRequest(session))
		byID[session.id] = session
	}
	req.SetSessions(items)
	resp, err := s.svcCtx.PresenceClient.RefreshUserSessions(ctx, req)
	if err != nil {
		outcome.recordPresenceFailure(err)
		if ctx.Err() == nil {
			logx.WithContext(ctx).Errorw("refresh session presences", logx.Field("count", len(items)), logx.Field("error", err))
		}
		return outcome
	}
	for _, sessionID := range resp.GetMissingSessionIds() {
		if session := byID[sessionID]; session != nil {
			if _, err := s.registerPresence(ctx, session); err != nil {
				outcome.recordPresenceFailure(err)
				if ctx.Err() == nil {
					logx.WithContext(ctx).Errorw("register missing session presence", logx.Field("session_id", sessionID), logx.Field("error", err))
				}
			}
		}
	}
	return outcome
}

func (o *leaseRefreshOutcome) merge(other leaseRefreshOutcome) {
	o.ownerFailures += other.ownerFailures
	o.presenceFailures += other.presenceFailures
	if o.ownerErrorType == "" {
		o.ownerErrorType = other.ownerErrorType
	}
	if o.presenceErrorType == "" {
		o.presenceErrorType = other.presenceErrorType
	}
}

func (o *leaseRefreshOutcome) recordOwnerFailure(err error) {
	o.ownerFailures++
	if o.ownerErrorType == "" {
		o.ownerErrorType = leaseRefreshErrorType(err)
	}
}

func (o *leaseRefreshOutcome) recordPresenceFailure(err error) {
	o.presenceFailures++
	if o.presenceErrorType == "" {
		o.presenceErrorType = leaseRefreshErrorType(err)
	}
}

func defaultLeaseErrorType(errorType string) string {
	if errorType == "" {
		return "none"
	}
	return errorType
}

func leaseRefreshErrorType(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	switch status.Code(err) {
	case codes.Canceled:
		return "canceled"
	case codes.DeadlineExceeded:
		return "timeout"
	case codes.Unavailable:
		return "unavailable"
	case codes.ResourceExhausted:
		return "resource_exhausted"
	default:
		return "internal"
	}
}

func presenceRefreshRequest(session *logicalSession) *presencev1.RefreshUserSessionRequest {
	session.mu.Lock()
	clientState := session.clientState
	session.mu.Unlock()
	req := new(presencev1.RefreshUserSessionRequest)
	req.SetUserId(session.userID)
	req.SetSessionId(session.id)
	req.SetGatewayId(session.gatewayID)
	req.SetGeneration(session.gatewayGeneration)
	req.SetDeviceType(session.deviceType)
	req.SetClientState(clientState)
	req.SetGuildIds(presenceGuildIDs(session))
	return req
}

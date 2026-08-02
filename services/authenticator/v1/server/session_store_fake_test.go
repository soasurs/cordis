package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/soasurs/cordis/services/authenticator/v1/internal/model"
	"github.com/soasurs/cordis/services/authenticator/v1/internal/store"
)

type fakeSessionStore struct {
	sessions            map[int64]*model.Session
	createdSession      *model.Session
	rotatedOldHash      string
	rotatedNewHash      string
	revokedSessionID    int64
	revokedOtherUserID  int64
	currentSessionID    int64
	factors             map[int64]*model.TOTPFactor
	enrollments         map[int64]*model.TOTPEnrollment
	challenges          map[string]*model.TwoFactorLoginChallenge
	recoveryCodes       map[int64]map[string]int64
	passwordResets      map[string]*model.PasswordResetToken
	passwordResetReads  []bool
	emailVerifications  map[string]*model.EmailVerificationToken
	registrationInvites map[string]*model.RegistrationInvite
	credentials         map[int64]*model.UserCredential
}

func newFakeSessionStore() *fakeSessionStore {
	return &fakeSessionStore{
		sessions:            make(map[int64]*model.Session),
		factors:             make(map[int64]*model.TOTPFactor),
		enrollments:         make(map[int64]*model.TOTPEnrollment),
		challenges:          make(map[string]*model.TwoFactorLoginChallenge),
		recoveryCodes:       make(map[int64]map[string]int64),
		passwordResets:      make(map[string]*model.PasswordResetToken),
		emailVerifications:  make(map[string]*model.EmailVerificationToken),
		registrationInvites: make(map[string]*model.RegistrationInvite),
		credentials:         make(map[int64]*model.UserCredential),
	}
}

func (s *fakeSessionStore) Transact(_ context.Context, fn func(store.Store) error) error {
	return fn(s)
}

func (s *fakeSessionStore) CreateSession(_ context.Context, params store.CreateSessionParams) (*model.Session, error) {
	session := &model.Session{
		SessionID: params.SessionID, UserID: params.UserID, RefreshTokenHash: params.RefreshTokenHash,
		RefreshTokenID: params.RefreshTokenID, RefreshTokenIssuedAt: params.RefreshTokenIssuedAt,
		RefreshTokenExpiresAt: params.RefreshTokenExpiresAt,
		UserAgent:             params.UserAgent, IP: params.IP, ExpiresAt: params.ExpiresAt,
		AbsoluteExpiresAt: params.AbsoluteExpiresAt,
	}
	s.createdSession = session
	s.sessions[params.SessionID] = session
	return session, nil
}

func (s *fakeSessionStore) GetSession(_ context.Context, sessionID int64) (*model.Session, error) {
	session, ok := s.sessions[sessionID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return session, nil
}

func (s *fakeSessionStore) ListSessions(_ context.Context, userID int64) ([]*model.Session, error) {
	sessions := make([]*model.Session, 0)
	for _, session := range s.sessions {
		if session.UserID == userID && session.RevokedAt == 0 && session.ExpiresAt > time.Now().UnixMilli() &&
			(session.AbsoluteExpiresAt == 0 || session.AbsoluteExpiresAt > time.Now().UnixMilli()) {
			sessions = append(sessions, session)
		}
	}
	return sessions, nil
}

func (s *fakeSessionStore) RotateRefreshToken(_ context.Context, params store.RotateRefreshTokenParams) error {
	session, ok := s.sessions[params.SessionID]
	if !ok || session.RefreshTokenHash != params.OldRefreshTokenHash {
		return sql.ErrNoRows
	}
	s.rotatedOldHash = params.OldRefreshTokenHash
	s.rotatedNewHash = params.NewRefreshTokenHash
	session.PreviousRefreshTokenHash = session.RefreshTokenHash
	session.PreviousRefreshTokenValidUntil = params.PreviousRefreshTokenValidUntil
	session.RefreshTokenHash = params.NewRefreshTokenHash
	session.RefreshTokenID = params.NewRefreshTokenID
	session.RefreshTokenIssuedAt = params.NewRefreshTokenIssuedAt
	session.RefreshTokenExpiresAt = params.NewRefreshTokenExpiresAt
	session.ExpiresAt = params.ExpiresAt
	return nil
}

func (s *fakeSessionStore) RevokeSession(_ context.Context, sessionID int64) error {
	session, ok := s.sessions[sessionID]
	if !ok {
		return sql.ErrNoRows
	}
	s.revokedSessionID = sessionID
	session.RevokedAt = time.Now().UnixMilli()
	return nil
}

func (s *fakeSessionStore) RevokeUserSession(_ context.Context, userID, sessionID int64) error {
	session, ok := s.sessions[sessionID]
	if !ok || session.UserID != userID || session.RevokedAt != 0 {
		return sql.ErrNoRows
	}
	s.revokedSessionID = sessionID
	session.RevokedAt = time.Now().UnixMilli()
	return nil
}

func (s *fakeSessionStore) RevokeOtherSessions(_ context.Context, userID, currentSessionID int64) (int64, error) {
	s.revokedOtherUserID = userID
	s.currentSessionID = currentSessionID
	var revoked int64
	for _, session := range s.sessions {
		if session.UserID == userID && session.SessionID != currentSessionID && session.RevokedAt == 0 {
			session.RevokedAt = time.Now().UnixMilli()
			revoked++
		}
	}
	return revoked, nil
}

func (s *fakeSessionStore) GetTOTPFactor(_ context.Context, userID int64, _ bool) (*model.TOTPFactor, error) {
	factor, ok := s.factors[userID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return factor, nil
}

func (s *fakeSessionStore) CreateTOTPEnrollment(_ context.Context, enrollment *model.TOTPEnrollment) error {
	if existing, ok := s.enrollments[enrollment.UserID]; ok && existing.ExpiresAt > enrollment.CreatedAt {
		return sql.ErrNoRows
	}
	s.enrollments[enrollment.UserID] = enrollment
	return nil
}

func (s *fakeSessionStore) GetTOTPEnrollment(_ context.Context, userID int64, tokenHash string, _ bool) (*model.TOTPEnrollment, error) {
	enrollment, ok := s.enrollments[userID]
	if !ok || enrollment.TokenHash != tokenHash {
		return nil, sql.ErrNoRows
	}
	return enrollment, nil
}

func (s *fakeSessionStore) DeleteTOTPEnrollment(_ context.Context, userID int64, tokenHash string) error {
	enrollment, ok := s.enrollments[userID]
	if !ok || enrollment.TokenHash != tokenHash {
		return sql.ErrNoRows
	}
	delete(s.enrollments, userID)
	return nil
}

func (s *fakeSessionStore) UpsertTOTPFactor(_ context.Context, factor *model.TOTPFactor) error {
	s.factors[factor.UserID] = factor
	return nil
}

func (s *fakeSessionStore) DeleteTOTPFactor(_ context.Context, userID int64) error {
	if _, ok := s.factors[userID]; !ok {
		return sql.ErrNoRows
	}
	delete(s.factors, userID)
	return nil
}

func (s *fakeSessionStore) UpdateTOTPLastUsedCounter(_ context.Context, userID, counter int64) error {
	factor, ok := s.factors[userID]
	if !ok || factor.LastUsedCounter >= counter {
		return sql.ErrNoRows
	}
	factor.LastUsedCounter = counter
	return nil
}

func (s *fakeSessionStore) CreateTwoFactorLoginChallenge(_ context.Context, challenge *model.TwoFactorLoginChallenge) error {
	s.challenges[challenge.TokenHash] = challenge
	return nil
}

func (s *fakeSessionStore) GetTwoFactorLoginChallenge(_ context.Context, tokenHash string, _ bool) (*model.TwoFactorLoginChallenge, error) {
	challenge, ok := s.challenges[tokenHash]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return challenge, nil
}

func (s *fakeSessionStore) IncrementTwoFactorLoginChallengeAttempts(_ context.Context, tokenHash string) error {
	challenge, ok := s.challenges[tokenHash]
	if !ok || challenge.ConsumedAt != 0 {
		return sql.ErrNoRows
	}
	challenge.Attempts++
	return nil
}

func (s *fakeSessionStore) ConsumeTwoFactorLoginChallenge(_ context.Context, tokenHash string) error {
	challenge, ok := s.challenges[tokenHash]
	if !ok || challenge.ConsumedAt != 0 {
		return sql.ErrNoRows
	}
	challenge.ConsumedAt = time.Now().UnixMilli()
	return nil
}

func (s *fakeSessionStore) ReplaceRecoveryCodes(_ context.Context, userID int64, codeHashes []string) error {
	codes := make(map[string]int64, len(codeHashes))
	for _, hash := range codeHashes {
		codes[hash] = 0
	}
	s.recoveryCodes[userID] = codes
	return nil
}

func (s *fakeSessionStore) CountUnusedRecoveryCodes(_ context.Context, userID int64) (int64, error) {
	var count int64
	for _, usedAt := range s.recoveryCodes[userID] {
		if usedAt == 0 {
			count++
		}
	}
	return count, nil
}

func (s *fakeSessionStore) ConsumeRecoveryCode(_ context.Context, userID int64, codeHash string) error {
	codes, ok := s.recoveryCodes[userID]
	if !ok || codes[codeHash] != 0 {
		return sql.ErrNoRows
	}
	codes[codeHash] = time.Now().UnixMilli()
	return nil
}

func (s *fakeSessionStore) UpsertPasswordResetToken(_ context.Context, token *model.PasswordResetToken) error {
	for hash, existing := range s.passwordResets {
		if existing.UserID == token.UserID {
			delete(s.passwordResets, hash)
		}
	}
	value := *token
	s.passwordResets[token.TokenHash] = &value
	return nil
}

func (s *fakeSessionStore) GetPasswordResetToken(_ context.Context, tokenHash string, forUpdate bool) (*model.PasswordResetToken, error) {
	s.passwordResetReads = append(s.passwordResetReads, forUpdate)
	token, ok := s.passwordResets[tokenHash]
	if !ok {
		return nil, sql.ErrNoRows
	}
	value := *token
	return &value, nil
}

func (s *fakeSessionStore) ConsumePasswordResetToken(_ context.Context, tokenHash string, consumedAt int64) error {
	token, ok := s.passwordResets[tokenHash]
	if !ok || token.ConsumedAt != 0 {
		return sql.ErrNoRows
	}
	token.ConsumedAt = consumedAt
	return nil
}

func (s *fakeSessionStore) UpsertEmailVerificationToken(_ context.Context, token *model.EmailVerificationToken) error {
	for hash, existing := range s.emailVerifications {
		if existing.UserID == token.UserID {
			delete(s.emailVerifications, hash)
		}
	}
	value := *token
	s.emailVerifications[token.TokenHash] = &value
	return nil
}

func (s *fakeSessionStore) GetEmailVerificationToken(_ context.Context, tokenHash string, _ bool) (*model.EmailVerificationToken, error) {
	token, ok := s.emailVerifications[tokenHash]
	if !ok {
		return nil, sql.ErrNoRows
	}
	value := *token
	return &value, nil
}

func (s *fakeSessionStore) ConsumeEmailVerificationToken(_ context.Context, tokenHash string, consumedAt int64) error {
	token, ok := s.emailVerifications[tokenHash]
	if !ok || token.ConsumedAt != 0 {
		return sql.ErrNoRows
	}
	token.ConsumedAt = consumedAt
	return nil
}

func (s *fakeSessionStore) CreateRegistrationInvite(_ context.Context, invite *model.RegistrationInvite) error {
	if _, ok := s.registrationInvites[invite.CodeHash]; ok {
		return errors.New("registration invite already exists")
	}
	value := *invite
	s.registrationInvites[invite.CodeHash] = &value
	return nil
}

func (s *fakeSessionStore) ReserveRegistrationInvite(
	_ context.Context,
	codeHash, email string,
	now, reservedUntil int64,
) (*model.RegistrationInvite, error) {
	invite, ok := s.registrationInvites[codeHash]
	if !ok ||
		invite.RevokedAt != 0 ||
		invite.RedeemedAt != 0 ||
		(invite.ExpiresAt != 0 && invite.ExpiresAt <= now) ||
		(invite.BoundEmail != "" && invite.BoundEmail != email) ||
		(invite.ReservedUntil > now && invite.ReservedEmail != email) {
		return nil, sql.ErrNoRows
	}
	invite.ReservedEmail = email
	invite.ReservedUntil = reservedUntil
	if invite.ExpiresAt != 0 && invite.ExpiresAt < invite.ReservedUntil {
		invite.ReservedUntil = invite.ExpiresAt
	}
	value := *invite
	return &value, nil
}

func (s *fakeSessionStore) RedeemRegistrationInvite(
	_ context.Context,
	inviteID int64,
	email string,
	userID, redeemedAt int64,
) error {
	for _, invite := range s.registrationInvites {
		if invite.ID == inviteID &&
			invite.ReservedEmail == email &&
			invite.ReservedUntil > redeemedAt &&
			invite.RevokedAt == 0 &&
			invite.RedeemedAt == 0 {
			invite.RedeemedUserID = userID
			invite.RedeemedAt = redeemedAt
			return nil
		}
	}
	return sql.ErrNoRows
}

func (s *fakeSessionStore) ReleaseRegistrationInvite(_ context.Context, inviteID int64, email string) error {
	for _, invite := range s.registrationInvites {
		if invite.ID == inviteID && invite.ReservedEmail == email && invite.RedeemedAt == 0 {
			invite.ReservedEmail = ""
			invite.ReservedUntil = 0
			return nil
		}
	}
	return sql.ErrNoRows
}

func (s *fakeSessionStore) ListRegistrationInvites(
	_ context.Context,
	beforeID int64,
	limit int,
) ([]*model.RegistrationInvite, error) {
	invites := make([]*model.RegistrationInvite, 0, len(s.registrationInvites))
	for _, invite := range s.registrationInvites {
		if beforeID == 0 || invite.ID < beforeID {
			value := *invite
			invites = append(invites, &value)
		}
	}
	if len(invites) > limit {
		invites = invites[:limit]
	}
	return invites, nil
}

func (s *fakeSessionStore) RevokeRegistrationInvite(_ context.Context, inviteID, revokedAt int64) error {
	for _, invite := range s.registrationInvites {
		if invite.ID == inviteID && invite.RevokedAt == 0 && invite.RedeemedAt == 0 {
			invite.RevokedAt = revokedAt
			return nil
		}
	}
	return sql.ErrNoRows
}

func (s *fakeSessionStore) CreateUserCredential(_ context.Context, credential *model.UserCredential) error {
	if _, ok := s.credentials[credential.UserID]; ok {
		return sql.ErrNoRows
	}
	value := *credential
	s.credentials[credential.UserID] = &value
	return nil
}

func (s *fakeSessionStore) GetUserCredential(_ context.Context, userID int64, _ bool) (*model.UserCredential, error) {
	credential, ok := s.credentials[userID]
	if !ok {
		return nil, sql.ErrNoRows
	}
	value := *credential
	return &value, nil
}

func (s *fakeSessionStore) UpdateUserCredential(_ context.Context, userID int64, hashedPassword string, updatedAt int64) error {
	credential, ok := s.credentials[userID]
	if !ok {
		return sql.ErrNoRows
	}
	credential.HashedPassword = hashedPassword
	credential.UpdatedAt = updatedAt
	return nil
}

// seedCredential stores a hashed credential so password checks run against
// the authenticator-owned store.

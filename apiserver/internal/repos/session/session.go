package repos

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"taskwiz.app/core/internal/models"
	database "taskwiz.app/core/internal/utils/database"
)

// Connection routing for this repository: everything runs on RW (primary).
// Session creation/validation is a hot read-your-writes path, and the
// remaining methods are writes.
type ISessionRepo interface {
	CreateSession(ctx context.Context, userID int, duration time.Duration) (string, error)
	ValidateSession(ctx context.Context, rawToken string) (*models.Session, error)
	DeleteSession(ctx context.Context, rawToken string) error
	DeleteUserSessions(ctx context.Context, userID int) error
	CleanupExpired(ctx context.Context) error
}

type SessionRepository struct {
	db *database.DB
}

var _ ISessionRepo = (*SessionRepository)(nil)

func NewSessionRepository(db *database.DB) ISessionRepo {
	return &SessionRepository{db: db}
}

func generateToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate session token: %s", err.Error())
	}
	return hex.EncodeToString(bytes), nil
}

func hashToken(rawToken string) string {
	h := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(h[:])
}

func (r *SessionRepository) CreateSession(ctx context.Context, userID int, duration time.Duration) (string, error) {
	rawToken, err := generateToken()
	if err != nil {
		return "", err
	}

	session := &models.Session{
		UserID:    userID,
		TokenHash: hashToken(rawToken),
		ExpiresAt: time.Now().UTC().Add(duration),
	}

	if err := r.db.RW().WithContext(ctx).Create(session).Error; err != nil {
		return "", fmt.Errorf("create session: %s", err.Error())
	}

	return rawToken, nil
}

func (r *SessionRepository) ValidateSession(ctx context.Context, rawToken string) (*models.Session, error) {
	var session models.Session
	tokenHash := hashToken(rawToken)

	if err := r.db.RW().WithContext(ctx).Where("token_hash = ?", tokenHash).First(&session).Error; err != nil {
		return nil, fmt.Errorf("session not found: %s", err.Error())
	}

	if time.Now().UTC().After(session.ExpiresAt) {
		_ = r.db.RW().WithContext(ctx).Delete(&session)
		return nil, fmt.Errorf("session has expired")
	}

	return &session, nil
}

func (r *SessionRepository) DeleteSession(ctx context.Context, rawToken string) error {
	tokenHash := hashToken(rawToken)
	return r.db.RW().WithContext(ctx).Where("token_hash = ?", tokenHash).Delete(&models.Session{}).Error
}

func (r *SessionRepository) DeleteUserSessions(ctx context.Context, userID int) error {
	return r.db.RW().WithContext(ctx).Where("user_id = ?", userID).Delete(&models.Session{}).Error
}

func (r *SessionRepository) CleanupExpired(ctx context.Context) error {
	return r.db.RW().WithContext(ctx).Where("expires_at < ?", time.Now().UTC()).Delete(&models.Session{}).Error
}

// Package invite manages single-use onboarding links redeemed through OAuth.
package invite

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

// ErrNotFound is returned by Revoke for unknown or redeemed invitations.
var ErrNotFound = errors.New("invite: not found")

// GenerateToken returns 32 cryptographically random bytes encoded as hex.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate invite token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// HashToken returns the SHA-256 hash of a raw invite token (hex-encoded).
func HashToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

type Service struct {
	repo repository.Repository
	// baseURL is the public dashboard origin used for OAuth redirects.
	baseURL string
	log     *slog.Logger
}

func New(repo repository.Repository, baseURL string, log *slog.Logger) *Service {
	return &Service{repo: repo, baseURL: baseURL, log: log.With("domain", "invite")}
}

// Create returns an invitation and its raw token, which cannot be recovered
// later.
func (s *Service) Create(ctx context.Context, createdBy, role string, ttl time.Duration, note *string) (string, *repository.Invite, error) {
	raw, err := GenerateToken()
	if err != nil {
		return "", nil, err
	}
	inv, err := s.repo.CreateInvite(ctx, &repository.InviteInput{
		TokenHash: HashToken(raw),
		Role:      role,
		Note:      note,
		CreatedBy: createdBy,
		ExpiresAt: time.Now().Add(ttl),
	})
	if err != nil {
		return "", nil, fmt.Errorf("create invite: %w", err)
	}
	s.log.Info("invite created", "id", inv.ID, "role", role, "created_by", createdBy, "expires_at", inv.ExpiresAt)
	return raw, inv, nil
}

// URL returns the dashboard redemption link for rawToken.
func (s *Service) URL(rawToken string) string {
	return s.baseURL + "/invite/" + rawToken
}

func (s *Service) List(ctx context.Context) ([]repository.Invite, error) {
	return s.repo.ListInvites(ctx)
}

// Revoke deletes an unredeemed invitation or returns ErrNotFound.
func (s *Service) Revoke(ctx context.Context, id int64) error {
	ok, err := s.repo.DeleteInvite(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotFound
	}
	s.log.Info("invite revoked", "id", id)
	return nil
}

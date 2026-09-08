package playbackauth

import (
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/session"
)

type Store interface {
	GetTwitchPlaybackSession(context.Context) (*repository.TwitchPlaybackSession, error)
	SaveTwitchPlaybackSession(context.Context, *repository.TwitchPlaybackSession) error
	UpdateTwitchPlaybackSessionValidation(context.Context, *repository.TwitchPlaybackSession) error
	DeleteTwitchPlaybackSession(context.Context) error
}

type Service struct {
	repo       Store
	key        []byte
	validator  Validator
	validation chan struct{}
}

// New returns a Service that encrypts the credential under a key derived from
// SESSION_SECRET, so the stored credential survives restarts and outlives any
// dashboard session. A missing or too-short secret leaves writes and decryption
// disabled.
func New(repo Store, secret string, validator Validator) *Service {
	s := &Service{repo: repo, validator: validator, validation: make(chan struct{}, 1)}
	secret = strings.TrimSpace(secret)
	if len(secret) >= 32 {
		if key, err := hkdf.Key(sha256.New, []byte(secret), nil, "replayvod-twitch-playback-session", 32); err == nil {
			s.key = key
		}
	}
	return s
}

// Status contains no credential, encrypted or otherwise.
type Status struct {
	State     string `json:"state"`
	Login     string `json:"login"`
	CheckedAt int64  `json:"checked_at"`
	ExpiresAt int64  `json:"expires_at"`
}

func status(row *repository.TwitchPlaybackSession) Status {
	if row == nil {
		return Status{State: "disconnected"}
	}
	state := "connected"
	if row.NeedsReconnect || (row.ExpiresAt > 0 && row.ExpiresAt <= time.Now().Unix()) {
		state = "reconnect_required"
	}
	return Status{State: state, Login: row.TwitchLogin, CheckedAt: row.CheckedAt, ExpiresAt: row.ExpiresAt}
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	row, err := s.repo.GetTwitchPlaybackSession(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return status(nil), nil
	}
	if err != nil {
		return Status{}, err
	}
	result := status(row)
	if _, err := session.Decrypt(s.key, row.EncryptedToken); err != nil {
		result.State = "reconnect_required"
	}
	return result, nil
}

func (s *Service) Connect(ctx context.Context, input string) (Status, error) {
	token, err := Normalize(input)
	if err != nil {
		return Status{}, err
	}
	if len(s.key) == 0 || s.validator == nil {
		return Status{}, ErrUnavailable
	}
	identity, err := s.validator.Validate(ctx, token)
	if err != nil {
		return Status{}, err
	}
	encrypted, err := session.Encrypt(s.key, []byte(token))
	if err != nil {
		return Status{}, errors.New("could not encrypt the Twitch session")
	}
	row := &repository.TwitchPlaybackSession{
		TwitchUserID: identity.UserID, TwitchLogin: identity.Login, EncryptedToken: encrypted,
		ExpiresAt: identity.ExpiresAt, CheckedAt: time.Now().Unix(),
	}
	if err := s.repo.SaveTwitchPlaybackSession(ctx, row); err != nil {
		return Status{}, err
	}
	return status(row), nil
}

func (s *Service) Disconnect(ctx context.Context) (Status, error) {
	if err := s.repo.DeleteTwitchPlaybackSession(ctx); err != nil {
		return Status{}, err
	}
	return status(nil), nil
}

// lockValidation coalesces simultaneous playback resolutions: a waiter reloads
// the row after the first request refreshes it. Waiting never delays shutdown.
// Connect/Disconnect deliberately do not take this lock; replacing a session
// must remain possible while Twitch validation is slow.
func (s *Service) lockValidation(ctx context.Context) error {
	select {
	case s.validation <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-s.validation
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) Check(ctx context.Context) (Status, error) {
	if err := s.lockValidation(ctx); err != nil {
		return Status{}, err
	}
	defer func() { <-s.validation }()
	_, err := s.currentToken(ctx, true)
	if err != nil && !errors.Is(err, ErrRejected) {
		return Status{}, err
	}
	return s.Status(ctx)
}

// Token returns the credential of the current connection, or "" when nothing is
// connected and playback goes out anonymous. It is read for every playback
// resolution, including resume and playlist renewals; a replacement race
// reloads the credential instead of failing the job.
func (s *Service) Token(ctx context.Context) (string, error) {
	if err := s.lockValidation(ctx); err != nil {
		return "", err
	}
	defer func() { <-s.validation }()
	return s.currentToken(ctx, false)
}

func (s *Service) currentToken(ctx context.Context, force bool) (string, error) {
	// Bound churn from repeated replacements; the downloader can back off and
	// try again. No recursion or retry of a known revoked/expired credential.
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		row, err := s.repo.GetTwitchPlaybackSession(ctx)
		if errors.Is(err, repository.ErrNotFound) {
			return "", nil
		}
		if err != nil {
			return "", storeUnavailable(ctx, "load playback session")
		}
		if status(row).State == "reconnect_required" {
			return "", ErrRejected
		}
		if force || row.CheckedAt <= time.Now().Add(-time.Hour).Unix() {
			token, err := s.validate(ctx, row)
			if errors.Is(err, ErrChanged) {
				force = false
				continue
			}
			return token, err
		}
		token, err := session.Decrypt(s.key, row.EncryptedToken)
		if err != nil {
			return "", ErrRejected
		}
		return string(token), nil
	}
	return "", ErrChanged
}

// RecheckRejected confirms GQL 401s at Twitch's validation endpoint. A stale
// rejection after disconnect/replacement requests a fresh playback resolution;
// it must neither invalidate the replacement nor kill its recording.
func (s *Service) RecheckRejected(ctx context.Context, rejectedToken string) error {
	if err := s.lockValidation(ctx); err != nil {
		return err
	}
	defer func() { <-s.validation }()
	row, err := s.repo.GetTwitchPlaybackSession(ctx)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrChanged
	}
	if err != nil {
		return storeUnavailable(ctx, "load playback session")
	}
	plain, err := session.Decrypt(s.key, row.EncryptedToken)
	if err != nil {
		return ErrRejected
	}
	if !bytes.Equal(plain, []byte(rejectedToken)) {
		return ErrChanged
	}
	if status(row).State == "reconnect_required" {
		return ErrRejected
	}
	_, err = s.validate(ctx, row)
	return err
}

func (s *Service) validate(ctx context.Context, row *repository.TwitchPlaybackSession) (string, error) {
	plain, err := session.Decrypt(s.key, row.EncryptedToken)
	if err != nil {
		return "", ErrRejected
	}
	if s.validator == nil {
		return "", ErrUnavailable
	}
	identity, err := s.validator.Validate(ctx, string(plain))
	if errors.Is(err, ErrWrongClient) {
		err = ErrRejected
	}
	if err == nil && identity.UserID != row.TwitchUserID {
		err = ErrRejected
	}
	// Persist only authoritative results, using the encrypted credential as a
	// compare-and-swap guard. Transient failures never change validation status.
	if err == nil || errors.Is(err, ErrRejected) {
		row.CheckedAt = time.Now().Unix()
		row.NeedsReconnect = errors.Is(err, ErrRejected)
		if err == nil {
			row.ExpiresAt = identity.ExpiresAt
		}
		if saveErr := s.repo.UpdateTwitchPlaybackSessionValidation(ctx, row); saveErr != nil {
			return "", storeUnavailable(ctx, "update playback session")
		}
	}
	// Re-read even on rejection/outage: the result belongs to the OLD row.
	current, readErr := s.repo.GetTwitchPlaybackSession(ctx)
	if errors.Is(readErr, repository.ErrNotFound) {
		return "", ErrChanged
	}
	if readErr != nil {
		return "", storeUnavailable(ctx, "load playback session")
	}
	if !bytes.Equal(current.EncryptedToken, row.EncryptedToken) {
		return "", ErrChanged
	}
	if status(current).State == "reconnect_required" {
		return "", ErrRejected
	}
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// Repository failures carry retry semantics without exposing SQL or credential
// data. Cancellation stays distinguishable so shutdown never starts a retry.
func storeUnavailable(ctx context.Context, operation string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrUnavailable, operation)
}

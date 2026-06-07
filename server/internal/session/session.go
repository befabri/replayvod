package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/befabri/replayvod/server/internal/repository"
)

const (
	CookieName    = "session_id"
	SessionMaxAge = 30 * 24 * time.Hour // 30 days
)

type TwitchTokens struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type Manager struct {
	repo         repository.Repository
	encKey       []byte
	secureCookie bool
	log          *slog.Logger
}

func NewManager(repo repository.Repository, sessionSecret string, secureCookie bool, log *slog.Logger) (*Manager, error) {
	sessionSecret = strings.TrimSpace(sessionSecret)
	if len(sessionSecret) < 32 {
		return nil, fmt.Errorf("session secret must be at least 32 characters")
	}
	key, err := deriveKey(sessionSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to derive session key: %w", err)
	}
	return &Manager{
		repo:         repo,
		encKey:       key,
		secureCookie: secureCookie,
		log:          log,
	}, nil
}

func (m *Manager) Create(ctx context.Context, w http.ResponseWriter, userID string, tokens *TwitchTokens, r *http.Request) error {
	rawID, err := GenerateSessionID()
	if err != nil {
		return err
	}

	encryptedTokens, err := m.encryptTokens(tokens)
	if err != nil {
		return err
	}

	ua := r.UserAgent()
	ip := r.RemoteAddr

	sess := &repository.Session{
		HashedID:        HashSessionID(rawID),
		UserID:          userID,
		EncryptedTokens: encryptedTokens,
		ExpiresAt:       time.Now().Add(SessionMaxAge),
		UserAgent:       &ua,
		IPAddress:       &ip,
	}

	if err := m.repo.CreateSession(ctx, sess); err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	m.setCookie(w, rawID)
	return nil
}

func (m *Manager) Get(ctx context.Context, r *http.Request) (*repository.Session, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return nil, nil // No cookie = not authenticated
	}

	hashedID := HashSessionID(cookie.Value)
	sess, err := m.repo.GetSession(ctx, hashedID)
	if err != nil {
		return nil, nil // Session not found = expired/revoked
	}

	if time.Now().After(sess.ExpiresAt) {
		m.repo.DeleteSession(ctx, hashedID)
		return nil, nil
	}

	return sess, nil
}

func (m *Manager) DecryptTokens(sess *repository.Session) (*TwitchTokens, error) {
	plaintext, err := Decrypt(m.encKey, sess.EncryptedTokens)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt tokens: %w", err)
	}

	var tokens TwitchTokens
	if err := json.Unmarshal(plaintext, &tokens); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tokens: %w", err)
	}
	return &tokens, nil
}

func (m *Manager) UpdateTokens(ctx context.Context, hashedID string, tokens *TwitchTokens) error {
	encrypted, err := m.encryptTokens(tokens)
	if err != nil {
		return err
	}
	return m.repo.UpdateSessionTokens(ctx, hashedID, encrypted)
}

func (m *Manager) Delete(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return nil
	}

	hashedID := HashSessionID(cookie.Value)
	if err := m.repo.DeleteSession(ctx, hashedID); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	m.clearCookie(w)
	return nil
}

func (m *Manager) DeleteByHash(ctx context.Context, hashedID string) error {
	if err := m.repo.DeleteSession(ctx, hashedID); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}
	return nil
}

func (m *Manager) ClearCookie() *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secureCookie,
		SameSite: http.SameSiteLaxMode,
	}
}

func (m *Manager) UpdateActivity(ctx context.Context, hashedID string) {
	if err := m.repo.UpdateSessionActivity(ctx, hashedID); err != nil {
		// A client that aborts its request (a video player canceling overlapping
		// Range GETs on every seek, say) cancels this same context mid-write.
		// That's the client's doing, not a write failure worth a warning.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return
		}
		m.log.Warn("failed to update session activity", "error", err)
	}
}

func (m *Manager) encryptTokens(tokens *TwitchTokens) ([]byte, error) {
	plaintext, err := json.Marshal(tokens)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal tokens: %w", err)
	}
	encrypted, err := Encrypt(m.encKey, plaintext)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt tokens: %w", err)
	}
	return encrypted, nil
}

func (m *Manager) setCookie(w http.ResponseWriter, rawID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    rawID,
		Path:     "/",
		MaxAge:   int(SessionMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   m.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

func (m *Manager) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secureCookie,
		SameSite: http.SameSiteLaxMode,
	})
}

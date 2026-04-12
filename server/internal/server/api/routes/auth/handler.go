package auth

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"time"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/go-chi/chi/v5"
)

const (
	stateCookieName    = "twitch_oauth_state"
	verifierCookieName = "twitch_oauth_verifier"
)

// Handler handles Twitch OAuth routes (Chi, not tRPC).
type Handler struct {
	cfg        *config.Config
	repo       repository.Repository
	twitch     *twitch.Client
	sessionMgr *session.Manager
	log        *slog.Logger
}

// NewHandler creates a new auth handler.
func NewHandler(cfg *config.Config, repo repository.Repository, tc *twitch.Client, sm *session.Manager, log *slog.Logger) *Handler {
	return &Handler{
		cfg:        cfg,
		repo:       repo,
		twitch:     tc,
		sessionMgr: sm,
		log:        log.With("domain", "auth"),
	}
}

// SetupRoutes registers OAuth routes on the Chi router.
func (h *Handler) SetupRoutes(r chi.Router) {
	r.Get("/auth/twitch", h.handleRedirect)
	r.Get("/auth/twitch/callback", h.handleCallback)
}

// handleRedirect redirects the user to Twitch's authorization page.
func (h *Handler) handleRedirect(w http.ResponseWriter, r *http.Request) {
	state, err := twitch.GenerateState()
	if err != nil {
		h.log.Error("failed to generate state", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	verifier, challenge, err := twitch.GeneratePKCE()
	if err != nil {
		h.log.Error("failed to generate pkce", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	secure := h.cfg.Env.Host != "localhost" && h.cfg.Env.Host != "0.0.0.0"

	// Store state in a short-lived cookie for verification
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	// Store PKCE verifier in a short-lived cookie (needed at token exchange)
	http.SetCookie(w, &http.Cookie{
		Name:     verifierCookieName,
		Value:    verifier,
		Path:     "/",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	authURL := h.twitch.AuthorizeURL(h.cfg.Env.CallbackURL, state, challenge, twitch.DefaultScopes)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// handleCallback handles the Twitch OAuth callback.
func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request) {
	// Verify state (constant-time)
	stateCookie, err := r.Cookie(stateCookieName)
	if err != nil || stateCookie.Value == "" {
		h.log.Warn("missing oauth state cookie")
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	queryState := r.URL.Query().Get("state")
	if subtle.ConstantTimeCompare([]byte(queryState), []byte(stateCookie.Value)) != 1 {
		h.log.Warn("oauth state mismatch")
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	// Read PKCE verifier (must match the challenge sent in the authorize URL)
	verifierCookie, err := r.Cookie(verifierCookieName)
	if err != nil || verifierCookie.Value == "" {
		h.log.Warn("missing pkce verifier cookie")
		http.Error(w, "invalid pkce", http.StatusBadRequest)
		return
	}
	codeVerifier := verifierCookie.Value

	// Clear state + verifier cookies
	http.SetCookie(w, &http.Cookie{
		Name:   stateCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:   verifierCookieName,
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	// Check for error from Twitch
	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		h.log.Warn("twitch oauth error", "error", errMsg, "description", r.URL.Query().Get("error_description"))
		http.Redirect(w, r, h.cfg.Env.FrontendURL+"/login?error="+errMsg, http.StatusTemporaryRedirect)
		return
	}

	// Exchange code for tokens
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	tokenResp, err := h.twitch.ExchangeCode(ctx, code, h.cfg.Env.CallbackURL, codeVerifier)
	if err != nil {
		h.log.Error("failed to exchange code", "error", err)
		http.Error(w, "failed to exchange code", http.StatusInternalServerError)
		return
	}

	// Fetch user profile
	users, err := h.twitch.GetUsers(twitch.WithUserToken(ctx, tokenResp.AccessToken), nil)
	if err != nil {
		h.log.Error("failed to fetch twitch user", "error", err)
		http.Error(w, "failed to fetch user", http.StatusInternalServerError)
		return
	}
	if len(users) == 0 {
		h.log.Error("twitch returned no user data")
		http.Error(w, "failed to fetch user", http.StatusInternalServerError)
		return
	}
	twitchUser := users[0]

	// Check whitelist
	if h.cfg.Env.WhitelistEnabled {
		whitelisted, err := h.repo.IsWhitelisted(ctx, twitchUser.ID)
		if err != nil {
			h.log.Error("whitelist check failed", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if !whitelisted {
			h.log.Info("user not whitelisted", "twitch_id", twitchUser.ID, "login", twitchUser.Login)
			http.Redirect(w, r, h.cfg.Env.FrontendURL+"/login?error=not_whitelisted", http.StatusTemporaryRedirect)
			return
		}
	}

	// Determine role: first user or OWNER_TWITCH_ID gets owner
	role := "viewer"
	if h.cfg.Env.OwnerTwitchID != "" && twitchUser.ID == h.cfg.Env.OwnerTwitchID {
		role = "owner"
	} else {
		// Check if this is the first user
		users, err := h.repo.ListUsers(ctx)
		if err == nil && len(users) == 0 {
			role = "owner"
		}
	}

	// Check if user already exists — preserve existing role
	existingUser, err := h.repo.GetUser(ctx, twitchUser.ID)
	if err == nil && existingUser != nil {
		role = existingUser.Role
	}

	// Upsert user
	email := &twitchUser.Email
	if twitchUser.Email == "" {
		email = nil
	}
	profileImg := &twitchUser.ProfileImageURL
	if twitchUser.ProfileImageURL == "" {
		profileImg = nil
	}

	_, err = h.repo.UpsertUser(ctx, &repository.User{
		ID:              twitchUser.ID,
		Login:           twitchUser.Login,
		DisplayName:     twitchUser.DisplayName,
		Email:           email,
		ProfileImageURL: profileImg,
		Role:            role,
	})
	if err != nil {
		h.log.Error("failed to upsert user", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Create session
	tokens := &session.TwitchTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}

	if err := h.sessionMgr.Create(ctx, w, twitchUser.ID, tokens, r); err != nil {
		h.log.Error("failed to create session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.log.Info("user authenticated", "twitch_id", twitchUser.ID, "login", twitchUser.Login, "role", role)

	// Redirect to dashboard
	http.Redirect(w, r, h.cfg.Env.FrontendURL+"/dashboard", http.StatusTemporaryRedirect)
}

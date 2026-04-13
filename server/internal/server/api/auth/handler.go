package auth

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"

	"github.com/befabri/replayvod/server/internal/config"
	"github.com/befabri/replayvod/server/internal/session"
	"github.com/befabri/replayvod/server/internal/twitch"
	"github.com/go-chi/chi/v5"
)

const (
	stateCookieName    = "twitch_oauth_state"
	verifierCookieName = "twitch_oauth_verifier"
)

// Handler serves the Twitch OAuth Chi routes. Thin by design: state
// and PKCE cookie plumbing live here (Chi concerns); the code exchange
// + whitelist + role + user upsert flow lives in Service.
type Handler struct {
	cfg        *config.Config
	twitch     *twitch.Client
	sessionMgr *session.Manager
	svc        *Service
	log        *slog.Logger
}

// NewHandler creates a new auth Chi handler wired to the domain
// Service.
func NewHandler(cfg *config.Config, tc *twitch.Client, sm *session.Manager, svc *Service, log *slog.Logger) *Handler {
	return &Handler{
		cfg:        cfg,
		twitch:     tc,
		sessionMgr: sm,
		svc:        svc,
		log:        log.With("domain", "auth"),
	}
}

// SetupRoutes registers OAuth routes on the Chi router.
func (h *Handler) SetupRoutes(r chi.Router) {
	r.Get("/auth/twitch", h.handleRedirect)
	r.Get("/auth/twitch/callback", h.handleCallback)
}

// handleRedirect generates state + PKCE, stashes them in short-lived
// cookies, and bounces the user to Twitch's authorize URL.
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
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   300, // 5 minutes
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
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

// handleCallback is the transport-layer counterpart to
// Service.HandleOAuthCallback: validates state + PKCE cookies, hands
// the code + verifier off to the service, and turns the result into a
// cookie + redirect. State tampering (constant-time compare), missing
// cookies, and provider-returned errors all short-circuit before the
// service gets invoked.
func (h *Handler) handleCallback(w http.ResponseWriter, r *http.Request) {
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

	verifierCookie, err := r.Cookie(verifierCookieName)
	if err != nil || verifierCookie.Value == "" {
		h.log.Warn("missing pkce verifier cookie")
		http.Error(w, "invalid pkce", http.StatusBadRequest)
		return
	}
	codeVerifier := verifierCookie.Value

	// Clear state + verifier cookies before running the exchange —
	// they're single-use and we don't want them lingering if the
	// exchange errors partway.
	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: verifierCookieName, Value: "", Path: "/", MaxAge: -1})

	if errMsg := r.URL.Query().Get("error"); errMsg != "" {
		h.log.Warn("twitch oauth error", "error", errMsg, "description", r.URL.Query().Get("error_description"))
		http.Redirect(w, r, h.cfg.Env.FrontendURL+"/login?error="+errMsg, http.StatusTemporaryRedirect)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	result, err := h.svc.HandleOAuthCallback(r.Context(), code, h.cfg.Env.CallbackURL, codeVerifier)
	if err != nil {
		var denied *ErrLoginDenied
		if errors.As(err, &denied) {
			http.Redirect(w, r, h.cfg.Env.FrontendURL+"/login?error="+denied.Reason, http.StatusTemporaryRedirect)
			return
		}
		h.log.Error("oauth callback failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := h.sessionMgr.Create(r.Context(), w, result.User.ID, result.Tokens, r); err != nil {
		h.log.Error("failed to create session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, h.cfg.Env.FrontendURL+"/dashboard", http.StatusTemporaryRedirect)
}

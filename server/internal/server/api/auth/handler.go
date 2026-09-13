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
	inviteCookieName   = "twitch_oauth_invite"
)

// Handler serves the Twitch OAuth routes.
type Handler struct {
	cfg        *config.Config
	twitch     *twitch.Client
	sessionMgr *session.Manager
	svc        *Service
	follows    FollowSync
	log        *slog.Logger
}

// FollowSync admits work to the application lifetime after session creation.
type FollowSync interface {
	Request(userID, accessToken string) error
}

// NewHandler requires a non-nil FollowSync owned by the application. Its owner
// must stop and join imports during shutdown; HTTP disconnects do not own them.
func NewHandler(cfg *config.Config, tc *twitch.Client, sm *session.Manager, svc *Service, follows FollowSync, log *slog.Logger) *Handler {
	if follows == nil {
		panic("follow synchronization service required")
	}
	return &Handler{
		cfg:        cfg,
		twitch:     tc,
		sessionMgr: sm,
		svc:        svc,
		follows:    follows,
		log:        log.With("domain", "auth"),
	}
}

func (h *Handler) SetupRoutes(r chi.Router) {
	r.Get("/auth/twitch", h.handleRedirect)
	r.Get("/auth/twitch/callback", h.handleCallback)
}

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

	// Ordinary login must clear abandoned invite cookies to avoid
	// unintended redemption.
	if inviteToken := r.URL.Query().Get("invite"); inviteToken != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     inviteCookieName,
			Value:    inviteToken,
			Path:     "/",
			MaxAge:   300, // 5 minutes
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})
	} else {
		http.SetCookie(w, &http.Cookie{Name: inviteCookieName, Value: "", Path: "/", MaxAge: -1})
	}

	authURL := h.twitch.AuthorizeURL(h.cfg.Env.CallbackURL, state, challenge, twitch.DefaultScopes)
	http.Redirect(w, r, authURL, http.StatusTemporaryRedirect)
}

// handleCallback validates state and PKCE before exchanging credentials or creating a session.
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

	inviteToken := ""
	if inviteCookie, err := r.Cookie(inviteCookieName); err == nil {
		inviteToken = inviteCookie.Value
	}

	// Clear single-use cookies before exchange so failures cannot leave
	// reusable credentials.
	http.SetCookie(w, &http.Cookie{Name: stateCookieName, Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: verifierCookieName, Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: inviteCookieName, Value: "", Path: "/", MaxAge: -1})

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

	result, err := h.svc.HandleOAuthCallback(r.Context(), code, h.cfg.Env.CallbackURL, codeVerifier, inviteToken)
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

	// Failed import admission must not invalidate a completed login.
	if err := h.follows.Request(result.User.ID, result.Tokens.AccessToken); err != nil {
		h.log.Warn("admit follow sync", "user_id", result.User.ID, "error", err)
	}

	http.Redirect(w, r, h.cfg.Env.FrontendURL+"/dashboard", http.StatusTemporaryRedirect)
}

package playbackauth

import (
	"context"
	"log/slog"

	svc "github.com/befabri/replayvod/server/internal/playbackauth"
	"github.com/befabri/replayvod/server/internal/server/api/apierr"
	"github.com/befabri/trpcgo"
)

type Handler struct {
	svc *svc.Service
	log *slog.Logger
}

type TwitchPlaybackStatusResponse struct {
	State     string `json:"state"`
	Login     string `json:"login"`
	CheckedAt int64  `json:"checked_at"`
	ExpiresAt int64  `json:"expires_at"`
}

type TwitchPlaybackConnectInput struct {
	SessionToken string `json:"session_token" validate:"required,max=1024"`
	// Consent is mandatory even for direct API callers. This session is used
	// by the entire shared recorder, not just the signed-in dashboard user.
	Consent bool `json:"consent"`
}

func (h *Handler) response(status svc.Status, err error) (TwitchPlaybackStatusResponse, error) {
	if err != nil {
		return TwitchPlaybackStatusResponse{}, apierr.Map(h.log, err, "update Twitch playback connection",
			apierr.OnVerbatim(svc.ErrInvalidInput, trpcgo.CodeBadRequest),
			apierr.OnVerbatim(svc.ErrWrongClient, trpcgo.CodeBadRequest),
			apierr.OnVerbatim(svc.ErrRejected, trpcgo.CodeBadRequest),
			apierr.OnVerbatim(svc.ErrUnavailable, trpcgo.CodeServiceUnavailable))
	}
	return TwitchPlaybackStatusResponse{State: status.State, Login: status.Login, CheckedAt: status.CheckedAt, ExpiresAt: status.ExpiresAt}, nil
}

func (h *Handler) Status(ctx context.Context) (TwitchPlaybackStatusResponse, error) {
	return h.response(h.svc.Status(ctx))
}

func (h *Handler) Connect(ctx context.Context, input TwitchPlaybackConnectInput) (TwitchPlaybackStatusResponse, error) {
	if !input.Consent {
		return TwitchPlaybackStatusResponse{}, trpcgo.NewError(trpcgo.CodeBadRequest, "consent is required to connect a Twitch session")
	}
	return h.response(h.svc.Connect(ctx, input.SessionToken))
}

func (h *Handler) Check(ctx context.Context) (TwitchPlaybackStatusResponse, error) {
	return h.response(h.svc.Check(ctx))
}

func (h *Handler) Disconnect(ctx context.Context) (TwitchPlaybackStatusResponse, error) {
	return h.response(h.svc.Disconnect(ctx))
}

package api

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/befabri/replayvod/server/internal/invite"
	"github.com/befabri/replayvod/server/internal/repository"
	"github.com/befabri/replayvod/server/internal/server/api/system"
)

func permissionResponse(t *testing.T, h *permissionHarness, method, path, body string, cookie *http.Cookie, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.com")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	if rr.Code != wantStatus {
		t.Fatalf("%s %s = %d, want %d: %s", method, path, rr.Code, wantStatus, rr.Body.String())
	}
	return rr
}

func decodePermissionData[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var envelope struct {
		Result struct {
			Data T `json:"data"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode tRPC response: %v: %s", err, rr.Body.String())
	}
	return envelope.Result.Data
}

func TestCreateInviteValidationOverHTTP(t *testing.T) {
	h := newPermissionHarness(t)
	for _, tc := range []struct {
		name string
		body string
	}{
		{"missing role", `{"ttl_minutes":60}`},
		{"owner role", `{"role":"owner","ttl_minutes":60}`},
		{"unknown role", `{"role":"superadmin","ttl_minutes":60}`},
		{"missing TTL", `{"role":"viewer"}`},
		{"negative TTL", `{"role":"viewer","ttl_minutes":-1}`},
		{"below minimum TTL", `{"role":"viewer","ttl_minutes":4}`},
		{"above maximum TTL", `{"role":"viewer","ttl_minutes":43201}`},
		{"fractional TTL", `{"role":"viewer","ttl_minutes":5.5}`},
		{"long note", fmt.Sprintf(`{"role":"viewer","ttl_minutes":60,"note":%q}`, strings.Repeat("é", 61))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			permissionResponse(t, h, http.MethodPost, "/trpc/system.createInvite", tc.body, h.owner, http.StatusBadRequest)
			rows, err := h.repo.ListInvites(context.Background())
			if err != nil || len(rows) != 0 {
				t.Fatalf("invalid input persisted an invite: %+v, %v", rows, err)
			}
		})
	}
	for _, tc := range []struct {
		role string
		ttl  int
	}{
		{"viewer", 5},
		{"admin", 43200},
	} {
		t.Run(fmt.Sprintf("valid %s TTL %d", tc.role, tc.ttl), func(t *testing.T) {
			note := strings.Repeat("é", 60)
			before := time.Now().Add(time.Duration(tc.ttl) * time.Minute).Truncate(time.Second)
			rr := permissionResponse(t, h, http.MethodPost, "/trpc/system.createInvite",
				fmt.Sprintf(`{"role":%q,"ttl_minutes":%d,"note":%q}`, tc.role, tc.ttl, note), h.admin, http.StatusOK)
			created := decodePermissionData[system.InviteCreatedInfo](t, rr)
			after := time.Now().Add(time.Duration(tc.ttl) * time.Minute)
			if created.ID == 0 || string(created.Role) != tc.role || created.ExpiresAt.Before(before) || created.ExpiresAt.After(after) {
				t.Fatalf("created invite = %+v; want role %s, expiry between %s and %s", created, tc.role, before, after)
			}
			const prefix = "http://localhost:3000/invite/"
			if !strings.HasPrefix(created.URL, prefix) {
				t.Fatalf("invite URL = %q", created.URL)
			}
			raw := strings.TrimPrefix(created.URL, prefix)
			decoded, err := hex.DecodeString(raw)
			if err != nil || len(decoded) != 32 {
				t.Fatalf("invite token must encode 32 random bytes: %q, %v", raw, err)
			}
			stored, err := h.repo.GetInviteByTokenHash(context.Background(), invite.HashToken(raw))
			if err != nil {
				t.Fatal(err)
			}
			if stored.ID != created.ID || stored.CreatedBy != "perm-admin-1" || stored.Note == nil || *stored.Note != note || !stored.ExpiresAt.Equal(created.ExpiresAt) || stored.RedeemedAt != nil {
				t.Fatalf("persisted invite differs from creation: %+v", stored)
			}
			if _, err := h.repo.GetInviteByTokenHash(context.Background(), raw); !errors.Is(err, repository.ErrNotFound) {
				t.Fatalf("raw token was stored as a lookup key: %v", err)
			}
		})
	}
}

func TestInviteListAndRevokeOverHTTP(t *testing.T) {
	h := newPermissionHarness(t)
	ctx := context.Background()
	empty := permissionResponse(t, h, http.MethodGet, "/trpc/system.listInvites", "", h.admin, http.StatusOK)
	if got := decodePermissionData[[]system.InviteInfo](t, empty); got == nil || len(got) != 0 {
		t.Fatalf("empty invite list = %+v, want []", got)
	}
	create := func() system.InviteCreatedInfo {
		return decodePermissionData[system.InviteCreatedInfo](t, permissionResponse(t, h, http.MethodPost, "/trpc/system.createInvite", `{"role":"viewer","ttl_minutes":60}`, h.admin, http.StatusOK))
	}
	pending, redeemed := create(), create()
	if pending.URL == redeemed.URL {
		t.Fatal("two invites share a raw token")
	}
	raw := strings.TrimPrefix(redeemed.URL, "http://localhost:3000/invite/")
	if ok, err := h.repo.RedeemInvite(ctx, invite.HashToken(raw), "perm-viewer-1"); err != nil || !ok {
		t.Fatalf("redeem = %v, %v", ok, err)
	}
	listed := permissionResponse(t, h, http.MethodGet, "/trpc/system.listInvites", "", h.admin, http.StatusOK)
	rows := decodePermissionData[[]system.InviteInfo](t, listed)
	if len(rows) != 2 || rows[0].ID != redeemed.ID || rows[1].ID != pending.ID || rows[0].RedeemedAt == nil || rows[0].RedeemedBy == nil || *rows[0].RedeemedBy != "perm-viewer-1" {
		t.Fatalf("listed invite audit data = %+v", rows)
	}
	for _, forbidden := range []string{raw, invite.HashToken(raw), pending.URL, `"token_hash"`, `"url"`} {
		if strings.Contains(listed.Body.String(), forbidden) {
			t.Fatalf("list response exposes invite credential %q", forbidden)
		}
	}
	revoke := func(id int64, status int) {
		permissionResponse(t, h, http.MethodPost, "/trpc/system.revokeInvite", fmt.Sprintf(`{"id":%d}`, id), h.admin, status)
	}
	revoke(pending.ID, http.StatusOK)
	revoke(pending.ID, http.StatusNotFound)
	revoke(redeemed.ID, http.StatusNotFound)
	rows = decodePermissionData[[]system.InviteInfo](t, permissionResponse(t, h, http.MethodGet, "/trpc/system.listInvites", "", h.admin, http.StatusOK))
	if len(rows) != 1 || rows[0].ID != redeemed.ID || rows[0].RedeemedBy == nil {
		t.Fatalf("revoke lost redeemed audit record: %+v", rows)
	}
	pendingRaw := strings.TrimPrefix(pending.URL, "http://localhost:3000/invite/")
	if ok, err := h.repo.RedeemInvite(ctx, invite.HashToken(pendingRaw), "perm-viewer-1"); err != nil || ok {
		t.Fatalf("revoked invite redeemed: %v, %v", ok, err)
	}
}

func TestRotateInviteOverHTTP(t *testing.T) {
	h := newPermissionHarness(t)
	ctx := context.Background()
	const prefix = "http://localhost:3000/invite/"
	rotate := func(id int64, status int) *httptest.ResponseRecorder {
		return permissionResponse(t, h, http.MethodPost, "/trpc/system.rotateInvite", fmt.Sprintf(`{"id":%d}`, id), h.admin, status)
	}

	for _, body := range []string{`{}`, `{"id":0}`, `{"id":"7"}`} {
		permissionResponse(t, h, http.MethodPost, "/trpc/system.rotateInvite", body, h.admin, http.StatusBadRequest)
	}
	rotate(99999, http.StatusNotFound)

	created := decodePermissionData[system.InviteCreatedInfo](t, permissionResponse(t, h, http.MethodPost, "/trpc/system.createInvite", `{"role":"admin","ttl_minutes":60,"note":"for bob"}`, h.admin, http.StatusOK))
	oldRaw := strings.TrimPrefix(created.URL, prefix)
	rotated := decodePermissionData[system.InviteCreatedInfo](t, rotate(created.ID, http.StatusOK))
	if rotated.ID != created.ID || rotated.Role != created.Role || !rotated.ExpiresAt.Equal(created.ExpiresAt) {
		t.Fatalf("rotated = %+v, want the same invite as %+v", rotated, created)
	}
	newRaw := strings.TrimPrefix(rotated.URL, prefix)
	if decoded, err := hex.DecodeString(newRaw); err != nil || len(decoded) != 32 || newRaw == oldRaw {
		t.Fatalf("rotated URL must carry a fresh 32-byte token: %q, %v", rotated.URL, err)
	}
	if _, err := h.repo.GetInviteByTokenHash(ctx, invite.HashToken(oldRaw)); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("old token still resolves: %v", err)
	}
	stored, err := h.repo.GetInviteByTokenHash(ctx, invite.HashToken(newRaw))
	if err != nil || stored.ID != created.ID || stored.CreatedBy != "perm-admin-1" || stored.Note == nil || *stored.Note != "for bob" || stored.RedeemedAt != nil {
		t.Fatalf("rotated row = %+v, %v; want the original invite under the new hash", stored, err)
	}
	listed := permissionResponse(t, h, http.MethodGet, "/trpc/system.listInvites", "", h.admin, http.StatusOK)
	for _, forbidden := range []string{oldRaw, newRaw, invite.HashToken(newRaw), `"url"`} {
		if strings.Contains(listed.Body.String(), forbidden) {
			t.Fatalf("list response exposes invite credential %q", forbidden)
		}
	}

	if ok, err := h.repo.RedeemInvite(ctx, invite.HashToken(newRaw), "perm-viewer-1"); err != nil || !ok {
		t.Fatalf("redeem rotated link = %v, %v", ok, err)
	}
	rotate(created.ID, http.StatusNotFound)

	expired, err := h.repo.CreateInvite(ctx, &repository.InviteInput{
		TokenHash: "rotate-http-expired", Role: "viewer", CreatedBy: "perm-admin-1",
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	rotate(expired.ID, http.StatusNotFound)
	if got, err := h.repo.GetInviteByTokenHash(ctx, "rotate-http-expired"); err != nil || got.ID != expired.ID {
		t.Fatalf("expired invite after rejected rotate = %+v, %v; want untouched", got, err)
	}
}

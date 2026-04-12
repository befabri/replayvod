package twitch

import (
	"errors"
	"net/http"
	"testing"
)

func TestNewAuthError_JSONObject(t *testing.T) {
	body := []byte(`{"error":"Unauthorized","error_code":"unauthorized_entitlements","message":"you can't watch this"}`)
	e := NewAuthError(403, body)
	if e.Code != "unauthorized_entitlements" {
		t.Errorf("Code=%q, want unauthorized_entitlements", e.Code)
	}
	if e.Message != "you can't watch this" {
		t.Errorf("Message=%q, want 'you can't watch this'", e.Message)
	}
	if e.Status != 403 {
		t.Errorf("Status=%d, want 403", e.Status)
	}
}

func TestNewAuthError_JSONArray(t *testing.T) {
	// Usher's 4xx body is typically a JSON array of per-channel
	// errors: [{"type":"error","error_code":"...","error":"..."}]
	body := []byte(`[{"error":"Forbidden","error_code":"vod_manifest_restricted"}]`)
	e := NewAuthError(403, body)
	if e.Code != "vod_manifest_restricted" {
		t.Errorf("Code=%q, want vod_manifest_restricted", e.Code)
	}
}

func TestNewAuthError_NonJSON(t *testing.T) {
	body := []byte("not json at all")
	e := NewAuthError(500, body)
	if e.Code != "" {
		t.Errorf("Code=%q, want empty on non-JSON body", e.Code)
	}
	if e.Message != "not json at all" {
		t.Errorf("Message=%q, want raw preview", e.Message)
	}
}

func TestClassifyAuthError_PermanentCodes(t *testing.T) {
	for _, code := range []string{
		"unauthorized_entitlements",
		"vod_manifest_restricted",
		"subscriptions_restricted",
		"subs_only_restricted",
	} {
		e := NewAuthError(403, []byte(`{"error_code":"`+code+`"}`))
		perm, isAuth := classifyAuthError(e)
		if !perm {
			t.Errorf("code=%s: expected permanent=true", code)
		}
		if !isAuth {
			t.Errorf("code=%s: expected isAuth=true", code)
		}
		if !IsPermanent(e) {
			t.Errorf("code=%s: IsPermanent=false, want true", code)
		}
	}
}

func TestClassifyAuthError_RetryableAuth(t *testing.T) {
	// Token-expiry style error: no recognized code, 401 status.
	// Retryable: caller should refresh + try again.
	e := NewAuthError(401, []byte(`{"error":"token expired"}`))
	perm, isAuth := classifyAuthError(e)
	if perm {
		t.Error("expected permanent=false for token-expiry")
	}
	if !isAuth {
		t.Error("expected isAuth=true for 401 AuthError")
	}
}

func TestClassifyAuthError_Non4xx(t *testing.T) {
	// 500 wrapped in AuthError shouldn't be treated as "auth" —
	// it's a server problem and belongs on the transport retry path.
	e := NewAuthError(500, []byte(""))
	perm, isAuth := classifyAuthError(e)
	if perm {
		t.Error("5xx shouldn't classify permanent")
	}
	if isAuth {
		t.Error("5xx shouldn't classify as isAuth")
	}
}

func TestClassifyAuthError_NilAndUnwrapped(t *testing.T) {
	perm, isAuth := classifyAuthError(nil)
	if perm || isAuth {
		t.Error("nil err: want (false, false)")
	}
	perm, isAuth = classifyAuthError(errors.New("plain"))
	if perm || isAuth {
		t.Error("plain err: want (false, false)")
	}
}

func TestAuthError_ErrorMessageShape(t *testing.T) {
	// Just make sure .Error() is always non-empty and includes
	// the status — helps operators find the right log entry.
	e := &AuthError{Status: http.StatusForbidden, Code: "foo", Message: "bar"}
	if e.Error() == "" {
		t.Error("Error() returned empty string")
	}
}

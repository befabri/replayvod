package twitch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/google/go-querystring/query"
)

//go:generate go run ../../tools/twitch-api-gen -out . -cache ../../tmp/reference.html -eventsub-ref-cache ../../tmp/eventsub-reference.html -eventsub-types-cache ../../tmp/eventsub-subscription-types.html

const (
	helixBaseURL = "https://api.twitch.tv/helix"
	authBaseURL  = "https://id.twitch.tv/oauth2"
)

// Client is the Twitch Helix API client.
type Client struct {
	clientID     string
	clientSecret string
	httpClient   *http.Client
	log          *slog.Logger

	appTokenMu  sync.Mutex
	appToken    string
	appTokenExp time.Time
}

// NewClient creates a new Twitch API client.
func NewClient(clientID, clientSecret string, log *slog.Logger) *Client {
	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		log: log.With("domain", "twitch"),
	}
}

// --- Context token plumbing ---

type userTokenCtxKey struct{}

// WithUserToken attaches a user access token to ctx. Generated Helix methods
// pick it up from the context automatically; when unset they fall back to the
// cached app access token.
func WithUserToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return context.WithValue(ctx, userTokenCtxKey{}, token)
}

func userTokenFrom(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(userTokenCtxKey{}).(string)
	return v, ok && v != ""
}

// --- App access token (client_credentials grant, cached) ---

// appAccessToken returns a cached app access token, refreshing when <5 min remain.
func (c *Client) appAccessToken(ctx context.Context) (string, error) {
	c.appTokenMu.Lock()
	defer c.appTokenMu.Unlock()
	if c.appToken != "" && time.Until(c.appTokenExp) > 5*time.Minute {
		return c.appToken, nil
	}
	resp, err := c.GetAppAccessToken(ctx)
	if err != nil {
		return "", err
	}
	c.appToken = resp.AccessToken
	c.appTokenExp = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	return c.appToken, nil
}

// --- Helix error ---

// HelixError is returned when the Twitch API responds with a non-2xx status.
type HelixError struct {
	Status int
	Body   string
}

func (e *HelixError) Error() string {
	return fmt.Sprintf("twitch: helix %d: %s", e.Status, e.Body)
}

// --- HTTP helpers used by generated_client.go ---

func (c *Client) get(ctx context.Context, path string, params any, out any) error {
	return c.do(ctx, http.MethodGet, path, params, nil, out)
}

func (c *Client) post(ctx context.Context, path string, params any, body any, out any) error {
	return c.do(ctx, http.MethodPost, path, params, body, out)
}

func (c *Client) put(ctx context.Context, path string, params any, body any, out any) error {
	return c.do(ctx, http.MethodPut, path, params, body, out)
}

func (c *Client) patch(ctx context.Context, path string, params any, body any, out any) error {
	return c.do(ctx, http.MethodPatch, path, params, body, out)
}

func (c *Client) delete(ctx context.Context, path string, params any, out any) error {
	return c.do(ctx, http.MethodDelete, path, params, nil, out)
}

func (c *Client) do(ctx context.Context, method, path string, params any, body any, out any) error {
	u, err := url.Parse(helixBaseURL + path)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if !isNilLike(params) {
		v, err := query.Values(params)
		if err != nil {
			return fmt.Errorf("encode query: %w", err)
		}
		u.RawQuery = v.Encode()
	}

	var bodyReader io.Reader
	if !isNilLike(body) {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}

	token, ok := userTokenFrom(ctx)
	if !ok {
		token, err = c.appAccessToken(ctx)
		if err != nil {
			return fmt.Errorf("acquire app token: %w", err)
		}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Client-Id", c.clientID)
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return &HelixError{Status: resp.StatusCode, Body: string(b)}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// isNilLike returns true for nil interfaces and typed-nil pointers.
func isNilLike(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Chan, reflect.Func:
		return rv.IsNil()
	}
	return false
}

// --- OAuth: kept hand-written ---

// TokenResponse is the response from the Twitch token endpoint.
type TokenResponse struct {
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
	ExpiresIn    int      `json:"expires_in"`
	Scope        []string `json:"scope"`
	TokenType    string   `json:"token_type"`
}

// ExchangeCode exchanges an authorization code for tokens using PKCE.
func (c *Client) ExchangeCode(ctx context.Context, code, redirectURI, codeVerifier string) (*TokenResponse, error) {
	data := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {redirectURI},
		"code_verifier": {codeVerifier},
	}
	return c.tokenRequest(ctx, data)
}

// RefreshUserToken refreshes a user's access token.
func (c *Client) RefreshUserToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	data := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	return c.tokenRequest(ctx, data)
}

// GetAppAccessToken obtains an app access token using client credentials.
// Callers that want the cached token should use appAccessToken instead.
func (c *Client) GetAppAccessToken(ctx context.Context) (*TokenResponse, error) {
	data := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"grant_type":    {"client_credentials"},
	}
	return c.tokenRequest(ctx, data)
}

func (c *Client) tokenRequest(ctx context.Context, data url.Values) (*TokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, authBaseURL+"/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token request error %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w", err)
	}
	return &tokenResp, nil
}

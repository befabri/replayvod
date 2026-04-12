package twitch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

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

// helixResponse is the standard Twitch Helix API response envelope.
type helixResponse[T any] struct {
	Data []T `json:"data"`
}

// User represents a Twitch user from the Helix API.
type User struct {
	ID              string `json:"id"`
	Login           string `json:"login"`
	DisplayName     string `json:"display_name"`
	Type            string `json:"type"`
	BroadcasterType string `json:"broadcaster_type"`
	Description     string `json:"description"`
	ProfileImageURL string `json:"profile_image_url"`
	OfflineImageURL string `json:"offline_image_url"`
	Email           string `json:"email"`
}

// GetUser fetches the authenticated user's profile using their access token.
func (c *Client) GetUser(ctx context.Context, accessToken string) (*User, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, helixBaseURL+"/users", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Client-Id", c.clientID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("twitch API error %d: %s", resp.StatusCode, string(body))
	}

	var result helixResponse[User]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode user response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("no user data returned")
	}

	return &result.Data[0], nil
}

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

package twitch

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
)

const (
	authorizeURL = "https://id.twitch.tv/oauth2/authorize"
)

// DefaultScopes are the Twitch OAuth scopes ReplayVOD needs.
var DefaultScopes = []string{
	"user:read:email",
}

// GenerateState generates a random state string for CSRF protection.
func GenerateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// GeneratePKCE generates a PKCE code verifier and its S256 challenge.
// The verifier is 32 random bytes, base64url-encoded (matches RFC 7636 43-128 char range).
// The challenge is SHA-256(verifier), base64url-encoded.
func GeneratePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("failed to generate pkce verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// AuthorizeURL builds the Twitch OAuth authorization URL with PKCE.
func (c *Client) AuthorizeURL(redirectURI, state, codeChallenge string, scopes []string) string {
	params := url.Values{
		"client_id":             {c.clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"scope":                 {joinScopes(scopes)},
		"state":                 {state},
		"code_challenge":        {codeChallenge},
		"code_challenge_method": {"S256"},
	}
	return authorizeURL + "?" + params.Encode()
}

func joinScopes(scopes []string) string {
	result := ""
	for i, s := range scopes {
		if i > 0 {
			result += " "
		}
		result += s
	}
	return result
}

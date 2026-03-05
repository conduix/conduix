package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

func TestOAuth2ClientCredentials(t *testing.T) {
	// Mock OAuth2 token server
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}

		grantType := r.FormValue("grant_type")
		if grantType != "client_credentials" {
			t.Errorf("expected grant_type=client_credentials, got %s", grantType)
		}

		clientID := r.FormValue("client_id")
		if clientID != "test-client" {
			t.Errorf("expected client_id=test-client, got %s", clientID)
		}

		resp := map[string]any{
			"access_token": "test-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer tokenServer.Close()

	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:         "oauth2",
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			TokenURL:     tokenServer.URL,
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	// Get token
	ctx := context.Background()
	token, err := source.getOAuth2Token(ctx)
	if err != nil {
		t.Fatalf("failed to get token: %v", err)
	}

	if token != "test-access-token" {
		t.Errorf("expected token 'test-access-token', got '%s'", token)
	}
}

func TestOAuth2RefreshToken(t *testing.T) {
	callCount := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}

		grantType := r.FormValue("grant_type")

		var resp map[string]any
		switch grantType {
		case "refresh_token":
			refreshToken := r.FormValue("refresh_token")
			if refreshToken != "initial-refresh-token" && refreshToken != "new-refresh-token" {
				t.Errorf("unexpected refresh_token: %s", refreshToken)
			}
			resp = map[string]any{
				"access_token":  "refreshed-access-token",
				"token_type":    "Bearer",
				"expires_in":    3600,
				"refresh_token": "new-refresh-token",
			}
		default:
			t.Errorf("unexpected grant_type: %s", grantType)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer tokenServer.Close()

	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:         "oauth2",
			ClientID:     "test-client",
			TokenURL:     tokenServer.URL,
			GrantType:    "refresh_token",
			RefreshToken: "initial-refresh-token",
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	ctx := context.Background()
	token, err := source.getOAuth2Token(ctx)
	if err != nil {
		t.Fatalf("failed to get token: %v", err)
	}

	if token != "refreshed-access-token" {
		t.Errorf("expected 'refreshed-access-token', got '%s'", token)
	}

	// Check that refresh token was updated
	newRefresh := source.GetRefreshToken()
	if newRefresh != "new-refresh-token" {
		t.Errorf("expected refresh token to be updated to 'new-refresh-token', got '%s'", newRefresh)
	}
}

func TestOAuth2TokenCaching(t *testing.T) {
	callCount := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]any{
			"access_token": "cached-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer tokenServer.Close()

	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:         "oauth2",
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			TokenURL:     tokenServer.URL,
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	ctx := context.Background()

	// First call - should hit server
	_, err = source.getOAuth2Token(ctx)
	if err != nil {
		t.Fatalf("failed to get token: %v", err)
	}

	// Second call - should use cache
	_, err = source.getOAuth2Token(ctx)
	if err != nil {
		t.Fatalf("failed to get token: %v", err)
	}

	// Third call - should use cache
	_, err = source.getOAuth2Token(ctx)
	if err != nil {
		t.Fatalf("failed to get token: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 token request (cached), got %d", callCount)
	}
}

func TestOAuth2TokenExpiry(t *testing.T) {
	callCount := 0
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		resp := map[string]any{
			"access_token": "token-" + string(rune('0'+callCount)),
			"token_type":   "Bearer",
			"expires_in":   1, // Very short expiry
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer tokenServer.Close()

	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:         "oauth2",
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			TokenURL:     tokenServer.URL,
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	ctx := context.Background()

	// First call
	_, err = source.getOAuth2Token(ctx)
	if err != nil {
		t.Fatalf("failed to get token: %v", err)
	}

	// Force expiry (since expires_in=1 and we subtract 60 seconds buffer, it's already expired)
	// Wait a bit and try again
	time.Sleep(100 * time.Millisecond)

	// Second call - should get new token due to expiry
	_, err = source.getOAuth2Token(ctx)
	if err != nil {
		t.Fatalf("failed to get token: %v", err)
	}

	// Should have made 2 calls since token expired immediately
	if callCount != 2 {
		t.Errorf("expected 2 token requests (expired), got %d", callCount)
	}
}

func TestPKCECodeVerifier(t *testing.T) {
	verifier := generateCodeVerifier()

	// RFC 7636: code_verifier should be 43-128 characters
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Errorf("code verifier length %d is outside valid range [43-128]", len(verifier))
	}

	// Should only contain unreserved characters
	for _, c := range verifier {
		if !isUnreservedChar(c) {
			t.Errorf("code verifier contains invalid character: %c", c)
		}
	}
}

func TestPKCECodeChallenge(t *testing.T) {
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

	// Test S256 method
	challenge := generateCodeChallenge(verifier, "S256")
	// Expected value from RFC 7636 example
	expected := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if challenge != expected {
		t.Errorf("S256 challenge mismatch: expected %s, got %s", expected, challenge)
	}

	// Test plain method
	plainChallenge := generateCodeChallenge(verifier, "plain")
	if plainChallenge != verifier {
		t.Errorf("plain challenge should equal verifier")
	}
}

func TestPKCEAuthURLGeneration(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:        "oauth2",
			ClientID:    "test-client",
			AuthURL:     "https://auth.example.com/authorize",
			TokenURL:    "https://auth.example.com/token",
			RedirectURL: "http://localhost:8080/callback",
			Scopes:      []string{"read", "write"},
			UsePKCE:     true,
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	authURL, err := source.GetPKCEAuthURL("test-state")
	if err != nil {
		t.Fatalf("failed to generate auth URL: %v", err)
	}

	// Verify URL contains required parameters
	if authURL == "" {
		t.Error("auth URL should not be empty")
	}

	// Check that code_challenge is present
	if !contains(authURL, "code_challenge=") {
		t.Error("auth URL should contain code_challenge")
	}
	if !contains(authURL, "code_challenge_method=S256") {
		t.Error("auth URL should contain code_challenge_method=S256")
	}
	if !contains(authURL, "state=test-state") {
		t.Error("auth URL should contain state parameter")
	}
	if !contains(authURL, "redirect_uri=") {
		t.Error("auth URL should contain redirect_uri")
	}
}

func TestSetRefreshToken(t *testing.T) {
	cfg := config.SourceV2{
		Type:   "http",
		URL:    "https://api.example.com/data",
		Method: "GET",
		Auth: &config.AuthConfig{
			Type:         "oauth2",
			ClientID:     "test-client",
			ClientSecret: "test-secret",
			TokenURL:     "https://auth.example.com/token",
		},
	}

	source, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("failed to create source: %v", err)
	}

	// Initially empty
	if source.GetRefreshToken() != "" {
		t.Error("refresh token should be empty initially")
	}

	// Set refresh token
	source.SetRefreshToken("my-refresh-token")

	// Should be updated
	if source.GetRefreshToken() != "my-refresh-token" {
		t.Error("refresh token should be 'my-refresh-token'")
	}
}

// Helper functions

func isUnreservedChar(c rune) bool {
	// RFC 7636: unreserved = ALPHA / DIGIT / "-" / "." / "_" / "~"
	if c >= 'A' && c <= 'Z' {
		return true
	}
	if c >= 'a' && c <= 'z' {
		return true
	}
	if c >= '0' && c <= '9' {
		return true
	}
	return c == '-' || c == '.' || c == '_' || c == '~'
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsString(s, substr))
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

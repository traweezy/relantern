package fakeprovider

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const testOAuthSecret = "0123456789abcdef0123456789abcdef"

func TestLocalOAuthAuthorizationCodeFlow(t *testing.T) {
	handler := newLocalOAuthTestHandler(t)
	verifier := strings.Repeat("v", 43)
	digest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(digest[:])
	authorizeURL := "/oauth/authorize?" + url.Values{
		"client_id":             {localOAuthClientID},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"redirect_uri":          {localOAuthRedirectURI},
		"response_type":         {"code"},
		"state":                 {"0123456789abcdef"},
	}.Encode()
	authorize := httptest.NewRecorder()
	handler.ServeHTTP(authorize, httptest.NewRequest(http.MethodGet, authorizeURL, nil))
	if authorize.Code != http.StatusFound {
		t.Fatalf("authorize status = %d, want %d", authorize.Code, http.StatusFound)
	}
	callback, err := url.Parse(authorize.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse callback: %v", err)
	}
	code := callback.Query().Get("code")
	if code == "" || callback.Query().Get("state") != "0123456789abcdef" {
		t.Fatalf("callback query = %q", callback.RawQuery)
	}

	form := url.Values{
		"client_id":     {localOAuthClientID},
		"client_secret": {testOAuthSecret},
		"code":          {code},
		"code_verifier": {verifier},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {localOAuthRedirectURI},
	}
	token := httptest.NewRecorder()
	tokenRequest := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	tokenRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(token, tokenRequest)
	if token.Code != http.StatusOK {
		t.Fatalf("token status = %d, want %d: %s", token.Code, http.StatusOK, token.Body.String())
	}
	var tokenBody struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(token.Body.Bytes(), &tokenBody); err != nil || tokenBody.AccessToken == "" {
		t.Fatalf("decode token: %v", err)
	}

	profile := httptest.NewRecorder()
	profileRequest := httptest.NewRequest(http.MethodGet, "/oauth/userinfo", nil)
	profileRequest.Header.Set("Authorization", "Bearer "+tokenBody.AccessToken)
	handler.ServeHTTP(profile, profileRequest)
	if profile.Code != http.StatusOK || !strings.Contains(profile.Body.String(), `"id":"5276132"`) {
		t.Fatalf("profile response = %d %s", profile.Code, profile.Body.String())
	}

	replay := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(form.Encode()))
	replayRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	handler.ServeHTTP(replay, replayRequest)
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("replayed code status = %d, want %d", replay.Code, http.StatusBadRequest)
	}
}

func TestLocalOAuthRejectsUnregisteredRedirect(t *testing.T) {
	handler := newLocalOAuthTestHandler(t)
	request := httptest.NewRequest(http.MethodGet, "/oauth/authorize?"+url.Values{
		"client_id":             {localOAuthClientID},
		"code_challenge":        {strings.Repeat("c", 43)},
		"code_challenge_method": {"S256"},
		"redirect_uri":          {"https://attacker.example/callback"},
		"response_type":         {"code"},
		"state":                 {"0123456789abcdef"},
	}.Encode(), nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest || response.Header().Get("Location") != "" {
		t.Fatalf("response = %d location %q", response.Code, response.Header().Get("Location"))
	}
}

func TestLocalOAuthConfigurationFailsClosed(t *testing.T) {
	_, err := NewWithOptions(KindSource, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		LocalOAuth: &LocalOAuthOptions{ClientSecret: "short", OwnerGitHubUserID: "not-numeric"},
	})
	if err == nil {
		t.Fatal("NewWithOptions() error = nil, want configuration error")
	}
}

func newLocalOAuthTestHandler(t *testing.T) http.Handler {
	t.Helper()
	handler, err := NewWithOptions(KindSource, slog.New(slog.NewTextHandler(io.Discard, nil)), Options{
		LocalOAuth: &LocalOAuthOptions{
			ClientSecret:      testOAuthSecret,
			OwnerGitHubUserID: "5276132",
		},
	})
	if err != nil {
		t.Fatalf("NewWithOptions() error = %v", err)
	}
	return handler
}

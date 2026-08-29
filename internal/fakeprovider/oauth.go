package fakeprovider

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/traweezy/relantern/internal/httpx"
)

const (
	localOAuthClientID    = "relantern-local"
	localOAuthRedirectURI = "http://127.0.0.1:3000/api/auth/callback/github"
	localOAuthLifetime    = 5 * time.Minute
	localOAuthStoreLimit  = 128
)

type LocalOAuthOptions struct {
	ClientSecret      string
	OwnerGitHubUserID string
}

type localOAuthGrant struct {
	codeChallenge string
	expiresAt     time.Time
}

type localOAuthStore struct {
	mu     sync.Mutex
	codes  map[string]localOAuthGrant
	tokens map[string]time.Time
}

func registerLocalOAuth(mux *http.ServeMux, options LocalOAuthOptions) error {
	if len(options.ClientSecret) < 32 || !positiveNumericID(options.OwnerGitHubUserID) {
		return &OAuthConfigurationError{}
	}
	store := &localOAuthStore{
		codes:  make(map[string]localOAuthGrant),
		tokens: make(map[string]time.Time),
	}
	mux.HandleFunc("GET /oauth/authorize", store.authorize)
	mux.HandleFunc("POST /oauth/token", store.token(options.ClientSecret))
	mux.HandleFunc("GET /oauth/userinfo", store.userInfo(options.OwnerGitHubUserID))
	return nil
}

type OAuthConfigurationError struct{}

func (*OAuthConfigurationError) Error() string {
	return "local OAuth configuration requires a bounded secret and numeric owner ID"
}

func (store *localOAuthStore) authorize(response http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	state := query.Get("state")
	challenge := query.Get("code_challenge")
	if query.Get("response_type") != "code" ||
		query.Get("client_id") != localOAuthClientID ||
		query.Get("redirect_uri") != localOAuthRedirectURI ||
		query.Get("code_challenge_method") != "S256" ||
		len(state) < 16 || len(state) > 2048 ||
		len(challenge) < 43 || len(challenge) > 128 {
		oauthProblem(response, request, http.StatusBadRequest, "invalid_request")
		return
	}
	code, err := randomOAuthValue()
	if err != nil {
		oauthProblem(response, request, http.StatusServiceUnavailable, "temporarily_unavailable")
		return
	}
	now := time.Now()
	store.mu.Lock()
	store.pruneExpired(now)
	if len(store.codes) >= localOAuthStoreLimit {
		store.mu.Unlock()
		oauthProblem(response, request, http.StatusTooManyRequests, "temporarily_unavailable")
		return
	}
	store.codes[code] = localOAuthGrant{codeChallenge: challenge, expiresAt: now.Add(localOAuthLifetime)}
	store.mu.Unlock()

	callback, _ := url.Parse(localOAuthRedirectURI)
	callbackQuery := callback.Query()
	callbackQuery.Set("code", code)
	callbackQuery.Set("state", state)
	callback.RawQuery = callbackQuery.Encode()
	response.Header().Set("Cache-Control", "no-store")
	http.Redirect(response, request, callback.String(), http.StatusFound)
}

func (store *localOAuthStore) token(clientSecret string) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		request.Body = http.MaxBytesReader(response, request.Body, 8<<10)
		if err := request.ParseForm(); err != nil ||
			request.Form.Get("grant_type") != "authorization_code" ||
			request.Form.Get("client_id") != localOAuthClientID ||
			request.Form.Get("redirect_uri") != localOAuthRedirectURI ||
			!constantTimeEqual(request.Form.Get("client_secret"), clientSecret) {
			oauthProblem(response, request, http.StatusBadRequest, "invalid_request")
			return
		}

		code := request.Form.Get("code")
		verifier := request.Form.Get("code_verifier")
		now := time.Now()
		store.mu.Lock()
		store.pruneExpired(now)
		grant, exists := store.codes[code]
		delete(store.codes, code)
		store.mu.Unlock()
		if !exists || !verifyPKCE(verifier, grant.codeChallenge) {
			oauthProblem(response, request, http.StatusBadRequest, "invalid_grant")
			return
		}

		accessToken, err := randomOAuthValue()
		if err != nil {
			oauthProblem(response, request, http.StatusServiceUnavailable, "temporarily_unavailable")
			return
		}
		store.mu.Lock()
		store.tokens[accessToken] = now.Add(localOAuthLifetime)
		store.mu.Unlock()
		response.Header().Set("Cache-Control", "no-store")
		httpx.WriteJSON(response, http.StatusOK, map[string]any{
			"access_token": accessToken,
			"expires_in":   int(localOAuthLifetime.Seconds()),
			"token_type":   "Bearer",
		})
	}
}

func (store *localOAuthStore) userInfo(ownerGitHubUserID string) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		authorization := strings.TrimSpace(request.Header.Get("Authorization"))
		accessToken, found := strings.CutPrefix(authorization, "Bearer ")
		now := time.Now()
		store.mu.Lock()
		store.pruneExpired(now)
		expiresAt, exists := store.tokens[accessToken]
		store.mu.Unlock()
		if !found || !exists || !expiresAt.After(now) {
			response.Header().Set("WWW-Authenticate", `Bearer realm="relantern-local"`)
			oauthProblem(response, request, http.StatusUnauthorized, "invalid_token")
			return
		}
		response.Header().Set("Cache-Control", "no-store")
		httpx.WriteJSON(response, http.StatusOK, map[string]any{
			"email":         ownerGitHubUserID + "@github.relantern.local",
			"emailVerified": true,
			"id":            ownerGitHubUserID,
			"login":         "traweezy",
			"name":          "Relantern owner",
		})
	}
}

func (store *localOAuthStore) pruneExpired(now time.Time) {
	for code, grant := range store.codes {
		if !grant.expiresAt.After(now) {
			delete(store.codes, code)
		}
	}
	for token, expiresAt := range store.tokens {
		if !expiresAt.After(now) {
			delete(store.tokens, token)
		}
	}
}

func randomOAuthValue() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func verifyPKCE(verifier string, challenge string) bool {
	if len(verifier) < 43 || len(verifier) > 128 {
		return false
	}
	digest := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(digest[:])
	return constantTimeEqual(expected, challenge)
}

func constantTimeEqual(left string, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func positiveNumericID(value string) bool {
	if value == "" || value[0] == '0' || len(value) > 16 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func oauthProblem(response http.ResponseWriter, request *http.Request, status int, code string) {
	response.Header().Set("Cache-Control", "no-store")
	httpx.WriteProblem(response, request, status, "OAuth request rejected", code)
}

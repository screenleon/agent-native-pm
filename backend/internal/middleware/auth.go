package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/screenleon/agent-native-pm/internal/models"
	"github.com/screenleon/agent-native-pm/internal/store"
)

type contextKey string

const contextKeyUser = contextKey("user")
const contextKeyAPIKey = contextKey("api-key")

// SessionAuth validates the session cookie or Authorization Bearer token.
// If valid, injects the user into the request context. Always calls next.
func SessionAuth(sessionStore *store.SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractToken(r)
			if token != "" {
				user, err := sessionStore.Validate(token)
				if err == nil && user != nil {
					r = r.WithContext(context.WithValue(r.Context(), contextKeyUser, user))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// APIKeyAuth validates X-API-Key header and stores the validated key in context.
// It also injects a synthetic member user so RequireAuth can pass when using API keys.
func APIKeyAuth(apiKeyStore *store.APIKeyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If user already set via session, skip
			if UserFromContext(r.Context()) != nil {
				next.ServeHTTP(w, r)
				return
			}
			key := r.Header.Get("X-API-Key")
			if key != "" {
				apiKey, err := apiKeyStore.Validate(key)
				if err == nil && apiKey != nil {
					r = r.WithContext(context.WithValue(r.Context(), contextKeyAPIKey, apiKey))
					// Inject a minimal agent-level user
					synthetic := &models.User{
						ID:       "api-key:" + apiKey.ID,
						Username: "api-key",
						Role:     "member",
						IsActive: true,
					}
					r = r.WithContext(context.WithValue(r.Context(), contextKeyUser, synthetic))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// InjectLocalAdmin injects a synthetic admin user on every request.
// Used in local mode to bypass the normal authentication flow.
func InjectLocalAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), contextKeyUser, &models.User{
			ID:       "local-admin",
			Username: "local",
			Role:     "admin",
			IsActive: true,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAPIKey returns 401 if no validated API key is in context.
func RequireAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if APIKeyFromContext(r.Context()) == nil {
			writeError(w, http.StatusUnauthorized, "api key required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuth returns 401 if no user is in context.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if UserFromContext(r.Context()) == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin returns 403 if the authenticated user is not an admin.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if user.Role != "admin" {
			writeError(w, http.StatusForbidden, "admin access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// UserFromContext retrieves the authenticated user from context.
func UserFromContext(ctx context.Context) *models.User {
	v := ctx.Value(contextKeyUser)
	if v == nil {
		return nil
	}
	u, _ := v.(*models.User)
	return u
}

// APIKeyFromContext retrieves the validated API key from context.
func APIKeyFromContext(ctx context.Context) *models.APIKey {
	v := ctx.Value(contextKeyAPIKey)
	if v == nil {
		return nil
	}
	k, _ := v.(*models.APIKey)
	return k
}

func extractToken(r *http.Request) string {
	// 1. Cookie
	if cookie, err := r.Cookie("session"); err == nil && cookie.Value != "" {
		return cookie.Value
	}
	// 2. Authorization Bearer
	auth := r.Header.Get("Authorization")
	if len(auth) > 7 && strings.EqualFold(auth[:7], "bearer ") {
		return auth[7:]
	}
	return ""
}

// writeError writes a JSON error envelope. Duplicated here to avoid import cycle.
func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(`{"data":null,"error":"` + msg + `","meta":{}}`))
}

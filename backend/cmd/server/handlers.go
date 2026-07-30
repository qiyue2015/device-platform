package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/httpjson"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	writeOK(w, map[string]string{"status": "ok"})
	return nil
}

func (a *app) handleReady(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	if !a.cfg.isInstalled() {
		writeOK(w, map[string]interface{}{
			"status": "setup_required",
			"checks": map[string]string{"setup": "required"},
		})
		return nil
	}
	writeOK(w, map[string]interface{}{
		"status": "ready",
		"checks": map[string]string{"config": "ok"},
	})
	return nil
}

func (a *app) handleLogin(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	if a.auth == nil {
		return newAPIError(http.StatusServiceUnavailable, "setup_required", "system setup is required")
	}
	metadata := authMetadataFromRequest(r)
	var req loginRequest
	if err := httpjson.DecodeStrict(r.Body, &req); err != nil {
		_, loginErr := a.auth.Login(r.Context(), "", "", metadata)
		return mapLoginError(w, loginErr)
	}
	user, err := a.auth.Login(r.Context(), req.Email, req.Password, metadata)
	if err != nil {
		return mapLoginError(w, err)
	}
	token, err := a.auth.IssueToken(user)
	if err != nil {
		return err
	}
	writeToken(w, token)
	return nil
}

func mapLoginError(w http.ResponseWriter, err error) error {
	var rateLimit authRateLimitError
	if errors.As(err, &rateLimit) {
		w.Header().Set("Retry-After", strconv.Itoa(rateLimit.RetryAfter))
		return newAPIError(http.StatusTooManyRequests, "rate_limited", "too many login attempts")
	}
	if errors.Is(err, errAuthDependencyUnavailable) {
		return newAPIError(http.StatusServiceUnavailable, "auth_dependency_unavailable", "authentication service unavailable")
	}
	return newAPIError(http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
}

func (a *app) handleRefresh(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	user, ok := userFromRequest(r)
	if !ok {
		return newAPIError(http.StatusUnauthorized, "unauthorized", "login required")
	}
	token, err := a.auth.IssueToken(user)
	if err != nil {
		return err
	}
	if err := a.auth.RecordRefresh(r.Context(), user, authMetadataFromRequest(r)); err != nil {
		if errors.Is(err, errAuthDependencyUnavailable) {
			return newAPIError(http.StatusServiceUnavailable, "auth_dependency_unavailable", "authentication service unavailable")
		}
		if errors.Is(err, errUnauthorized) {
			return newAPIError(http.StatusUnauthorized, "unauthorized", "login required")
		}
		return err
	}
	writeToken(w, token)
	return nil
}

func writeToken(w http.ResponseWriter, token string) {
	writeOK(w, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   int(tokenTTL / time.Second),
	})
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	user, ok := userFromRequest(r)
	if !ok {
		return newAPIError(http.StatusUnauthorized, "unauthorized", "login required")
	}
	if err := a.auth.Logout(r.Context(), user, authMetadataFromRequest(r)); err != nil {
		if errors.Is(err, errAuthDependencyUnavailable) {
			return newAPIError(http.StatusServiceUnavailable, "auth_dependency_unavailable", "authentication service unavailable")
		}
		return newAPIError(http.StatusUnauthorized, "unauthorized", "login required")
	}
	writeOK(w, map[string]bool{"logged_out": true})
	return nil
}

func authMetadataFromRequest(r *http.Request) authRequestMetadata {
	return authRequestMetadata{
		IPAddress:       clientIP(r),
		RequestID:       httpjson.RequestID(r.Context()),
		ClientRequestID: httpjson.ClientRequestID(r.Context()),
	}
}

func (a *app) handleMe(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	user, ok := userFromRequest(r)
	if !ok {
		return newAPIError(http.StatusUnauthorized, "unauthorized", "login required")
	}
	writeOK(w, map[string]interface{}{
		"id":              user.ID,
		"name":            user.DisplayName,
		"nickname":        user.DisplayName,
		"email":           user.Email,
		"email_verified":  true,
		"mobile":          "",
		"mobile_verified": false,
		"roles":           []string{"admin"},
	})
	return nil
}

func (a *app) handleMenu(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	writeOK(w, []interface{}{})
	return nil
}

func (a *app) handleSetupStatus(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	writeOK(w, getSetupStatus())
	return nil
}

func (a *app) handleSetupTestDB(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	if err := ensureSetupAllowed(); err != nil {
		return err
	}
	var req databaseSetupRequest
	if err := httpjson.DecodeStrict(r.Body, &req); err != nil {
		return newAPIError(http.StatusBadRequest, "invalid_request", "invalid request")
	}
	if err := testDatabaseConnection(r.Context(), req.URL); err != nil {
		return newAPIError(http.StatusBadRequest, "database_unavailable", err.Error())
	}
	writeOK(w, map[string]string{"message": "database connection successful"})
	return nil
}

func (a *app) handleSetupTestRedis(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	if err := ensureSetupAllowed(); err != nil {
		return err
	}
	var req redisSetupRequest
	if err := httpjson.DecodeStrict(r.Body, &req); err != nil {
		return newAPIError(http.StatusBadRequest, "invalid_request", "invalid request")
	}
	if err := testRedisConnection(r.Context(), req.URL); err != nil {
		return newAPIError(http.StatusBadRequest, "redis_unavailable", err.Error())
	}
	writeOK(w, map[string]string{"message": "redis connection successful"})
	return nil
}

func (a *app) handleSetupInstall(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	var req setupInstallRequest
	if err := httpjson.DecodeStrict(r.Body, &req); err != nil {
		return newAPIError(http.StatusBadRequest, "invalid_request", "invalid request")
	}
	result, err := performInstall(r.Context(), req)
	if err != nil {
		return err
	}
	a.cfg.DatabaseURL = result.DatabaseURL
	a.cfg.RedisURL = result.RedisURL
	a.cfg.JWTSecret = result.JWTSecret
	a.cfg.Installed = true
	if a.db != nil {
		_ = a.db.Close()
	}
	db, err := sql.Open("postgres", result.DatabaseURL)
	if err != nil {
		return err
	}
	a.db = db
	a.auth = newDBAuthenticator(db, result.JWTSecret)
	writeOK(w, map[string]bool{"installed": true})
	return nil
}

func (a *app) handleAdminPlaceholder(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	writeOK(w, map[string]string{
		"namespace": "admin",
		"path":      strings.TrimPrefix(r.URL.Path, "/v1/admin/"),
	})
	return nil
}

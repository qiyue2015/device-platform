package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/commandservice"
	"github.com/qiyue2015/device-platform/internal/deviceservice"
	"github.com/qiyue2015/device-platform/internal/httpjson"
	"github.com/qiyue2015/device-platform/internal/projectservice"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
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
	if !a.runtimeInstalled() {
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
	auth := a.authenticationService()
	if auth == nil {
		return newAPIError(http.StatusServiceUnavailable, "setup_required", "system setup is required")
	}
	metadata := authMetadataFromRequest(r)
	var req loginRequest
	if err := httpjson.DecodeStrict(r.Body, &req); err != nil {
		_, loginErr := auth.Login(r.Context(), "", "", metadata)
		return mapLoginError(w, loginErr)
	}
	user, err := auth.Login(r.Context(), req.Email, req.Password, metadata)
	if err != nil {
		return mapLoginError(w, err)
	}
	token, err := auth.IssueToken(user)
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
	auth := a.authenticationService()
	if auth == nil {
		return newAPIError(http.StatusUnauthorized, "unauthorized", "login required")
	}
	token, err := auth.IssueToken(user)
	if err != nil {
		return err
	}
	if err := auth.RecordRefresh(r.Context(), user, authMetadataFromRequest(r)); err != nil {
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
	auth := a.authenticationService()
	if auth == nil {
		return newAPIError(http.StatusUnauthorized, "unauthorized", "login required")
	}
	if err := auth.Logout(r.Context(), user, authMetadataFromRequest(r)); err != nil {
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
	installed := a.runtimeInstalled()
	writeOK(w, setupStatus{NeedsSetup: !installed, Installed: installed, Step: "system"})
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
		return newAPIError(http.StatusBadRequest, "invalid_install_request", "invalid setup request")
	}
	if err := validateDatabaseURL(req.URL); err != nil {
		return newAPIError(http.StatusBadRequest, "invalid_install_request", "invalid setup request")
	}
	if err := testDatabaseConnection(r.Context(), req.URL); err != nil {
		return newAPIError(http.StatusBadRequest, "database_unavailable", "database is unavailable")
	}
	writeOK(w, map[string]bool{"reachable": true})
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
		return newAPIError(http.StatusBadRequest, "invalid_install_request", "invalid setup request")
	}
	if _, err := redisOptionsFromURL(req.URL); err != nil {
		return newAPIError(http.StatusBadRequest, "invalid_install_request", "invalid setup request")
	}
	if err := testRedisConnection(r.Context(), req.URL); err != nil {
		return newAPIError(http.StatusBadRequest, "redis_unavailable", "Redis is unavailable")
	}
	writeOK(w, map[string]bool{"reachable": true})
	return nil
}

func (a *app) handleSetupInstall(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	var req setupInstallRequest
	if err := httpjson.DecodeStrict(r.Body, &req); err != nil {
		return newAPIError(http.StatusBadRequest, "invalid_install_request", "invalid installation request")
	}
	result, err := performInstall(r.Context(), req)
	if err != nil {
		return err
	}
	cfg := a.runtimeConfig()
	cfg.DatabaseURL = result.DatabaseURL
	cfg.RedisURL = result.RedisURL
	cfg.JWTSecret = result.JWTSecret
	cfg.WebhookSecretEncryptionKey = append([]byte(nil), result.WebhookSecretEncryptionKey...)
	cfg.Installed = true
	db, err := sql.Open("postgres", result.DatabaseURL)
	if err != nil {
		return err
	}
	store := repository.NewPostgresStore(db)
	projects, err := projectservice.New(store, projectservice.Config{
		EncryptionKeys: map[int][]byte{1: result.WebhookSecretEncryptionKey}, ActiveEncryptionKeyVersion: 1,
	})
	if err != nil {
		_ = db.Close()
		return err
	}
	devices, err := deviceservice.New(store, deviceServiceConfig(cfg))
	if err != nil {
		_ = db.Close()
		return err
	}
	commands, err := commandservice.New(store, commandServiceConfig(devices))
	if err != nil {
		_ = db.Close()
		return err
	}
	worker, err := newPersistentCommandWorker(store, a.cloudProviders)
	if err != nil {
		_ = db.Close()
		return err
	}
	webhookWorker, err := newPersistentWebhookWorker(store, projects, cfg)
	if err != nil {
		_ = db.Close()
		return err
	}
	a.stopMemoryWebhookWorker()
	if previousDB := a.replaceRuntime(cfg, db, newDBAuthenticator(db, result.JWTSecret), projects, devices, commands); previousDB != nil && previousDB != db {
		a.replaceCommandWorker(worker)
		a.replaceWebhookWorker(webhookWorker)
		_ = previousDB.Close()
	} else {
		a.replaceCommandWorker(worker)
		a.replaceWebhookWorker(webhookWorker)
	}
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

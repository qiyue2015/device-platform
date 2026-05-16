package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
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
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newAPIError(http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
	}
	if a.auth == nil {
		return newAPIError(http.StatusServiceUnavailable, "setup_required", "system setup is required")
	}
	user, err := a.auth.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		return newAPIError(http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
	}
	token, err := a.auth.IssueToken(user)
	if err != nil {
		return err
	}
	writeToken(w, token)
	return nil
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
	writeToken(w, token)
	return nil
}

func writeToken(w http.ResponseWriter, token string) {
	writeOK(w, map[string]string{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   "86400",
	})
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	writeOK(w, map[string]string{"logout_url": ""})
	return nil
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

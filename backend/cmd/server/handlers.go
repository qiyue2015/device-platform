package main

import (
	"crypto/subtle"
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
	if !a.adminCredentialsMatch(req.Email, req.Password) {
		return newAPIError(http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
	}
	writeToken(w, a.cfg.AdminAccessToken)
	return nil
}

func (a *app) handleRefresh(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodPost {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	writeToken(w, a.cfg.AdminAccessToken)
	return nil
}

func (a *app) adminCredentialsMatch(email, password string) bool {
	emailOK := subtle.ConstantTimeCompare([]byte(strings.TrimSpace(email)), []byte(a.cfg.AdminEmail)) == 1
	passwordOK := subtle.ConstantTimeCompare([]byte(password), []byte(a.cfg.AdminPassword)) == 1
	return emailOK && passwordOK
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

func (a *app) handleLegacyMe(w http.ResponseWriter, r *http.Request) error {
	if r.Method == http.MethodGet {
		return a.handleMe(w, r)
	}
	return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func (a *app) handleLegacyMenu(w http.ResponseWriter, r *http.Request) error {
	if r.Method == http.MethodGet {
		return a.handleMenu(w, r)
	}
	return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}

func (a *app) handleMe(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	writeOK(w, map[string]interface{}{
		"id":              "admin",
		"name":            "Admin",
		"nickname":        "Device Platform Admin",
		"email":           "admin@example.com",
		"email_verified":  true,
		"mobile":          "",
		"mobile_verified": false,
		"roles":           []string{"admin", "super-admin"},
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

func (a *app) handleOpenPlaceholder(w http.ResponseWriter, r *http.Request) error {
	if r.Method != http.MethodGet {
		return newAPIError(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	}
	writeOK(w, map[string]string{
		"namespace":  "open",
		"path":       strings.TrimPrefix(r.URL.Path, "/v1/open/"),
		"project_id": projectIDFromContext(r.Context()),
	})
	return nil
}

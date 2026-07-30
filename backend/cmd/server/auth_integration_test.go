//go:build integration

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/storage"
	"golang.org/x/crypto/bcrypt"
)

const (
	authTestAdminID  = "10000000-0000-0000-0000-000000000001"
	authTestEmail    = "admin@example.test"
	authTestPassword = "StrongPass123!"
)

func TestDBAuthenticatorPersistsSessionInvalidationAndSecurityAudit(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		seedAuthTestAdmin(t, db)
		now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
		auth := newDBAuthenticator(db, testJWTSecret)
		auth.now = func() time.Time { return now }
		metadata := authRequestMetadata{
			IPAddress:       "192.0.2.10",
			RequestID:       "10000000-0000-4000-8000-000000000010",
			ClientRequestID: "client-auth-10",
		}

		user, err := auth.Login(context.Background(), authTestEmail, authTestPassword, metadata)
		if err != nil {
			t.Fatal(err)
		}
		token, err := auth.IssueToken(user)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := auth.ParseToken(context.Background(), token); err != nil {
			t.Fatalf("fresh DB token rejected: %v", err)
		}
		if err := auth.RecordRefresh(context.Background(), user, metadata); err != nil {
			t.Fatal(err)
		}
		if err := auth.Logout(context.Background(), user, metadata); err != nil {
			t.Fatal(err)
		}
		if _, err := auth.ParseToken(context.Background(), token); !errors.Is(err, errUnauthorized) {
			t.Fatalf("logged-out DB token error = %v", err)
		}
		if err := auth.RecordRefresh(context.Background(), user, metadata); !errors.Is(err, errUnauthorized) {
			t.Fatalf("logged-out session refresh audit error = %v", err)
		}

		var generation int64
		if err := db.QueryRow(`SELECT session_generation FROM users WHERE id = $1`, authTestAdminID).Scan(&generation); err != nil {
			t.Fatal(err)
		}
		if generation != 1 {
			t.Fatalf("session generation = %d", generation)
		}
		var loginCount, refreshCount, logoutCount int
		if err := db.QueryRow(`
			SELECT
				count(*) FILTER (WHERE action = 'auth.login' AND result = 'success'),
				count(*) FILTER (WHERE action = 'auth.refresh' AND result = 'success'),
				count(*) FILTER (WHERE action = 'auth.logout' AND result = 'success')
			FROM audit_logs
		`).Scan(&loginCount, &refreshCount, &logoutCount); err != nil {
			t.Fatal(err)
		}
		if loginCount != 1 || refreshCount != 1 || logoutCount != 1 {
			t.Fatalf("auth audit counts = login:%d refresh:%d logout:%d", loginCount, refreshCount, logoutCount)
		}
		var requestID, ipAddress, clientRequestID string
		if err := db.QueryRow(`
			SELECT request_id, host(ip_address), metadata->>'client_request_id'
			FROM audit_logs WHERE action = 'auth.logout'
		`).Scan(&requestID, &ipAddress, &clientRequestID); err != nil {
			t.Fatal(err)
		}
		if requestID != metadata.RequestID || ipAddress != metadata.IPAddress || clientRequestID != metadata.ClientRequestID {
			t.Fatalf("logout audit metadata drift: request=%q ip=%q client=%q", requestID, ipAddress, clientRequestID)
		}
	})
}

func TestDBAuthenticatorRateLimitsConcurrentEmailIPAndPersistsIPLimit(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		seedAuthTestAdmin(t, db)
		db.SetMaxOpenConns(24)
		now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
		auth := newDBAuthenticator(db, testJWTSecret)
		auth.now = func() time.Time { return now }
		metadata := authRequestMetadata{IPAddress: "192.0.2.20", RequestID: "10000000-0000-4000-8000-000000000020"}

		var wait sync.WaitGroup
		var mu sync.Mutex
		invalidCount := 0
		rateLimitedCount := 0
		unexpected := []error{}
		for index := 0; index < 10; index++ {
			wait.Add(1)
			go func() {
				defer wait.Done()
				_, err := auth.Login(context.Background(), authTestEmail, "wrong-password", metadata)
				mu.Lock()
				defer mu.Unlock()
				var rateLimit authRateLimitError
				switch {
				case errors.Is(err, errInvalidCredentials):
					invalidCount++
				case errors.As(err, &rateLimit):
					if rateLimit.RetryAfter < 1 || rateLimit.RetryAfter > int(authRateLimitWindow/time.Second) {
						unexpected = append(unexpected, fmt.Errorf("retry after %d", rateLimit.RetryAfter))
					}
					rateLimitedCount++
				default:
					unexpected = append(unexpected, err)
				}
			}()
		}
		wait.Wait()
		if invalidCount != authEmailIPLimit || rateLimitedCount != 10-authEmailIPLimit || len(unexpected) != 0 {
			t.Fatalf("concurrent limit results invalid=%d limited=%d unexpected=%v", invalidCount, rateLimitedCount, unexpected)
		}

		restarted := newDBAuthenticator(db, testJWTSecret)
		restarted.now = auth.now
		if _, err := restarted.Login(context.Background(), authTestEmail, authTestPassword, metadata); !isRateLimited(err) {
			t.Fatalf("rate limit must survive authenticator restart, got %v", err)
		}

		if _, err := db.Exec(`DELETE FROM auth_login_failure_events`); err != nil {
			t.Fatal(err)
		}
		ipMetadata := authRequestMetadata{IPAddress: "192.0.2.21", RequestID: "10000000-0000-4000-8000-000000000021"}
		for index := 0; index < authIPLimit; index++ {
			email := fmt.Sprintf("unknown-%02d@example.test", index)
			if _, err := auth.Login(context.Background(), email, "wrong-password", ipMetadata); !errors.Is(err, errInvalidCredentials) {
				t.Fatalf("IP failure %d error = %v", index+1, err)
			}
		}
		if _, err := auth.Login(context.Background(), "another@example.test", "wrong-password", ipMetadata); !isRateLimited(err) {
			t.Fatalf("IP-wide limit error = %v", err)
		}

		auth.now = func() time.Time { return now.Add(authRateLimitWindow + time.Second) }
		for index := 0; index < 3; index++ {
			if _, err := auth.Login(context.Background(), authTestEmail, "wrong-password", metadata); !errors.Is(err, errInvalidCredentials) {
				t.Fatalf("post-expiry failure %d error = %v", index+1, err)
			}
		}
		if _, err := auth.Login(context.Background(), authTestEmail, authTestPassword, metadata); err != nil {
			t.Fatalf("expired rate limit must permit login: %v", err)
		}
		var emailFailures, ipFailures int
		if err := db.QueryRow(`
			SELECT
				count(*) FILTER (WHERE scope = 'email_ip' AND key_digest = $1),
				count(*) FILTER (WHERE scope = 'ip' AND key_digest = $2)
			FROM auth_login_failure_events
		`, authDigest(authTestEmail+"\x00"+metadata.IPAddress), authDigest(metadata.IPAddress)).Scan(&emailFailures, &ipFailures); err != nil {
			t.Fatal(err)
		}
		if emailFailures != 0 || ipFailures != 3 {
			t.Fatalf("successful login must clear only email+IP failures: email=%d ip=%d", emailFailures, ipFailures)
		}
	})
}

func TestMalformedLoginRequestsAreAuditedAndRateLimited(t *testing.T) {
	withAuthTestDatabase(t, func(db *sql.DB) {
		seedAuthTestAdmin(t, db)
		auth := newDBAuthenticator(db, testJWTSecret)
		auth.now = func() time.Time { return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC) }
		application := &app{auth: auth}

		for attempt := 1; attempt <= authEmailIPLimit+1; attempt++ {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(`{"email":`))
			request.RemoteAddr = "192.0.2.30:12345"
			err := application.handleLogin(recorder, request)
			var responseErr apiError
			if !errors.As(err, &responseErr) {
				t.Fatalf("attempt %d error = %v", attempt, err)
			}
			if attempt <= authEmailIPLimit && responseErr.status != http.StatusUnauthorized {
				t.Fatalf("attempt %d status = %d", attempt, responseErr.status)
			}
			if attempt > authEmailIPLimit {
				if responseErr.status != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") == "" {
					t.Fatalf("limited attempt status=%d retry-after=%q", responseErr.status, recorder.Header().Get("Retry-After"))
				}
			}
		}

		var failures, audits int
		if err := db.QueryRow(`SELECT count(*) FROM auth_login_failure_events WHERE scope = 'ip'`).Scan(&failures); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow(`SELECT count(*) FROM audit_logs WHERE action = 'auth.login' AND result = 'failure'`).Scan(&audits); err != nil {
			t.Fatal(err)
		}
		if failures != authEmailIPLimit || audits != authEmailIPLimit+1 {
			t.Fatalf("malformed login persistence failures=%d audits=%d", failures, audits)
		}
	})
}

func TestRefreshAndLogoutAreLinearizedInBothLockOrders(t *testing.T) {
	tests := []struct {
		name        string
		first       string
		wantRefresh error
	}{
		{name: "refresh commits first", first: "refresh"},
		{name: "logout commits first", first: "logout", wantRefresh: errUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withAuthTestDatabase(t, func(db *sql.DB) {
				seedAuthTestAdmin(t, db)
				db.SetMaxOpenConns(4)
				auth := newDBAuthenticator(db, testJWTSecret)
				auth.now = func() time.Time { return time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC) }
				user, err := auth.Login(context.Background(), authTestEmail, authTestPassword, authRequestMetadata{})
				if err != nil {
					t.Fatal(err)
				}

				locked := make(chan struct{})
				release := make(chan struct{})
				auth.afterSessionLocked = func(action string) {
					if action == tt.first {
						close(locked)
						<-release
					}
				}
				refreshResult := make(chan error, 1)
				logoutResult := make(chan error, 1)
				if tt.first == "refresh" {
					go func() { refreshResult <- auth.RecordRefresh(context.Background(), user, authRequestMetadata{}) }()
					<-locked
					go func() { logoutResult <- auth.Logout(context.Background(), user, authRequestMetadata{}) }()
				} else {
					go func() { logoutResult <- auth.Logout(context.Background(), user, authRequestMetadata{}) }()
					<-locked
					go func() { refreshResult <- auth.RecordRefresh(context.Background(), user, authRequestMetadata{}) }()
				}
				close(release)

				if err := <-logoutResult; err != nil {
					t.Fatalf("logout error = %v", err)
				}
				refreshErr := <-refreshResult
				if !errors.Is(refreshErr, tt.wantRefresh) {
					t.Fatalf("refresh error = %v, want %v", refreshErr, tt.wantRefresh)
				}

				var refreshAudits int
				if err := db.QueryRow(`SELECT count(*) FROM audit_logs WHERE action = 'auth.refresh'`).Scan(&refreshAudits); err != nil {
					t.Fatal(err)
				}
				wantRefreshAudits := 1
				if tt.wantRefresh != nil {
					wantRefreshAudits = 0
				}
				if refreshAudits != wantRefreshAudits {
					t.Fatalf("refresh audit count = %d, want %d", refreshAudits, wantRefreshAudits)
				}
			})
		})
	}
}

func isRateLimited(err error) bool {
	var rateLimit authRateLimitError
	return errors.As(err, &rateLimit)
}

func seedAuthTestAdmin(t *testing.T, db *sql.DB) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(authTestPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO users (id, email, password_hash, display_name, is_admin)
		VALUES ($1, $2, $3, 'Test Admin', true)
	`, authTestAdminID, authTestEmail, string(hash)); err != nil {
		t.Fatal(err)
	}
}

func withAuthTestDatabase(t *testing.T, fn func(*sql.DB)) {
	t.Helper()
	baseURL := requireServerMigrationTestDatabase(t)
	admin, err := sql.Open("postgres", baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("server_contract_test_auth_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop auth test schema: %v", err)
		}
	}()

	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.ApplyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	fn(db)
}

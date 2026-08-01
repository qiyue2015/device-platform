package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	minJWTSecretLength     = 32
	tokenTTL               = 24 * time.Hour
	authRateLimitWindow    = 15 * time.Minute
	authEmailIPLimit       = 5
	authIPLimit            = 20
	jwtIssuer              = "device-platform"
	jwtAudience            = "device-platform-admin"
	defaultMemoryJWTSecret = "0123456789abcdef0123456789abcdef"
)

var (
	errInvalidCredentials        = errors.New("invalid credentials")
	errUnauthorized              = errors.New("unauthorized")
	errAuthDependencyUnavailable = errors.New("authentication dependency unavailable")
	dummyPasswordHash            = mustHashDummyPassword()
)

type authRateLimitError struct {
	RetryAfter int
}

func (e authRateLimitError) Error() string { return "authentication rate limit exceeded" }

type authRequestMetadata struct {
	IPAddress       string
	RequestID       string
	ClientRequestID string
}

type currentUser struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Nickname          string `json:"nickname"`
	Email             string `json:"email"`
	DisplayName       string `json:"display_name"`
	IsAdmin           bool   `json:"is_admin"`
	SessionGeneration int64  `json:"-"`
}

type authenticator interface {
	Login(ctx context.Context, email, password string, metadata authRequestMetadata) (currentUser, error)
	IssueToken(user currentUser) (string, error)
	ParseToken(ctx context.Context, token string) (currentUser, error)
	RecordRefresh(ctx context.Context, user currentUser, metadata authRequestMetadata) error
	Logout(ctx context.Context, user currentUser, metadata authRequestMetadata) error
}

type dbAuthenticator struct {
	db                 *sql.DB
	secret             string
	now                func() time.Time
	afterSessionLocked func(action string)
}

func newDBAuthenticator(db *sql.DB, secret string) dbAuthenticator {
	return dbAuthenticator{db: db, secret: secret, now: time.Now}
}

func (a dbAuthenticator) Login(ctx context.Context, email, password string, metadata authRequestMetadata) (currentUser, error) {
	return loginAdmin(ctx, a.db, email, password, metadata, a.now().UTC())
}

func (a dbAuthenticator) IssueToken(user currentUser) (string, error) {
	return createJWT(user, a.secret, a.now().UTC())
}

func (a dbAuthenticator) ParseToken(ctx context.Context, token string) (currentUser, error) {
	claims, err := parseJWT(token, a.secret, a.now().UTC())
	if err != nil {
		return currentUser{}, errUnauthorized
	}
	var user currentUser
	err = a.db.QueryRowContext(ctx, `
		SELECT id::text, email, display_name, is_admin, session_generation
		FROM users WHERE id = $1
	`, claims.ID).Scan(&user.ID, &user.Email, &user.DisplayName, &user.IsAdmin, &user.SessionGeneration)
	if errors.Is(err, sql.ErrNoRows) {
		return currentUser{}, errUnauthorized
	}
	if err != nil {
		return currentUser{}, fmt.Errorf("%w: validate session", errAuthDependencyUnavailable)
	}
	if !user.IsAdmin || user.SessionGeneration != claims.SessionGeneration {
		return currentUser{}, errUnauthorized
	}
	user.Name = user.DisplayName
	user.Nickname = user.DisplayName
	return user, nil
}

func (a dbAuthenticator) RecordRefresh(ctx context.Context, user currentUser, metadata authRequestMetadata) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin refresh", errAuthDependencyUnavailable)
	}
	defer tx.Rollback()
	var generation int64
	err = tx.QueryRowContext(ctx, `
		SELECT session_generation FROM users WHERE id = $1 AND is_admin FOR SHARE
	`, user.ID).Scan(&generation)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && generation != user.SessionGeneration) {
		return errUnauthorized
	}
	if err != nil {
		return fmt.Errorf("%w: validate refresh session", errAuthDependencyUnavailable)
	}
	if a.afterSessionLocked != nil {
		a.afterSessionLocked("refresh")
	}
	if err := insertAuthAudit(ctx, tx, "auth.refresh", "success", &user.ID, metadata, a.now().UTC()); err != nil {
		return fmt.Errorf("%w: record refresh audit", errAuthDependencyUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit refresh", errAuthDependencyUnavailable)
	}
	return nil
}

func (a dbAuthenticator) Logout(ctx context.Context, user currentUser, metadata authRequestMetadata) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: begin logout", errAuthDependencyUnavailable)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE users
		SET session_generation = session_generation + 1, updated_at = $1
		WHERE id = $2 AND session_generation = $3 AND is_admin
	`, a.now().UTC(), user.ID, user.SessionGeneration)
	if err != nil {
		return fmt.Errorf("%w: invalidate session", errAuthDependencyUnavailable)
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%w: inspect logout", errAuthDependencyUnavailable)
	}
	if updated != 1 {
		return errUnauthorized
	}
	if a.afterSessionLocked != nil {
		a.afterSessionLocked("logout")
	}
	if err := insertAuthAudit(ctx, tx, "auth.logout", "success", &user.ID, metadata, a.now().UTC()); err != nil {
		return fmt.Errorf("%w: record logout audit", errAuthDependencyUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: commit logout", errAuthDependencyUnavailable)
	}
	return nil
}

type memoryAuthenticator struct {
	mu           sync.Mutex
	user         currentUser
	passwordHash string
	secret       string
	now          func() time.Time
}

func newMemoryAuthenticator(email, displayName, password, secret string) (*memoryAuthenticator, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	if displayName == "" {
		displayName = "Test Admin"
	}
	return &memoryAuthenticator{
		user: currentUser{
			ID:          "test-admin",
			Name:        displayName,
			Nickname:    displayName,
			Email:       email,
			DisplayName: displayName,
			IsAdmin:     true,
		},
		passwordHash: hash,
		secret:       secret,
		now:          time.Now,
	}, nil
}

func (a *memoryAuthenticator) Login(_ context.Context, email, password string, _ authRequestMetadata) (currentUser, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if strings.EqualFold(strings.TrimSpace(email), a.user.Email) && checkPassword(a.passwordHash, password) {
		return a.user, nil
	}
	return currentUser{}, errInvalidCredentials
}

func (a *memoryAuthenticator) IssueToken(user currentUser) (string, error) {
	return createJWT(user, a.secret, a.now().UTC())
}

func (a *memoryAuthenticator) ParseToken(_ context.Context, token string) (currentUser, error) {
	claims, err := parseJWT(token, a.secret, a.now().UTC())
	if err != nil {
		return currentUser{}, errUnauthorized
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if claims.ID != a.user.ID || claims.SessionGeneration != a.user.SessionGeneration {
		return currentUser{}, errUnauthorized
	}
	return a.user, nil
}

func (a *memoryAuthenticator) RecordRefresh(_ context.Context, _ currentUser, _ authRequestMetadata) error {
	return nil
}

func (a *memoryAuthenticator) Logout(_ context.Context, user currentUser, _ authRequestMetadata) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if user.ID != a.user.ID || user.SessionGeneration != a.user.SessionGeneration {
		return errUnauthorized
	}
	a.user.SessionGeneration++
	return nil
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func checkPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func mustHashDummyPassword() string {
	hash, err := bcrypt.GenerateFromPassword([]byte("device-platform-invalid-login"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(hash)
}

func createJWT(user currentUser, secret string, now time.Time) (string, error) {
	if len(secret) < minJWTSecretLength {
		return "", fmt.Errorf("jwt secret is too short")
	}
	jti, err := randomUUID()
	if err != nil {
		return "", fmt.Errorf("generate token identifier: %w", err)
	}
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := map[string]any{
		"iss":                jwtIssuer,
		"aud":                jwtAudience,
		"sub":                user.ID,
		"email":              user.Email,
		"name":               user.DisplayName,
		"is_admin":           user.IsAdmin,
		"session_generation": user.SessionGeneration,
		"iat":                now.Unix(),
		"exp":                now.Add(tokenTTL).Unix(),
		"jti":                jti,
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return signingInput + "." + signature, nil
}

func parseJWT(token, secret string, now time.Time) (currentUser, error) {
	if len(secret) < minJWTSecretLength {
		return currentUser{}, errors.New("invalid token configuration")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return currentUser{}, errors.New("invalid token")
	}
	signingInput := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	expected := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(got, expected) {
		return currentUser{}, errors.New("invalid token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return currentUser{}, errors.New("invalid token payload")
	}
	headerPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return currentUser{}, errors.New("invalid token header")
	}
	var header struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}
	if err := json.Unmarshal(headerPayload, &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return currentUser{}, errors.New("invalid token header")
	}
	var claims struct {
		Issuer            string `json:"iss"`
		Audience          string `json:"aud"`
		Subject           string `json:"sub"`
		Email             string `json:"email"`
		Name              string `json:"name"`
		IsAdmin           bool   `json:"is_admin"`
		SessionGeneration int64  `json:"session_generation"`
		IssuedAt          int64  `json:"iat"`
		Expires           int64  `json:"exp"`
		TokenID           string `json:"jti"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return currentUser{}, errors.New("invalid token claims")
	}
	if claims.Issuer != jwtIssuer || claims.Audience != jwtAudience || claims.Subject == "" || claims.Email == "" || !claims.IsAdmin || claims.TokenID == "" || claims.SessionGeneration < 0 {
		return currentUser{}, errors.New("invalid token user")
	}
	if claims.IssuedAt <= 0 || claims.IssuedAt > now.Unix() || claims.Expires <= now.Unix() || claims.Expires <= claims.IssuedAt {
		return currentUser{}, errors.New("token expired")
	}
	return currentUser{
		ID:                claims.Subject,
		Name:              claims.Name,
		Nickname:          claims.Name,
		Email:             claims.Email,
		DisplayName:       claims.Name,
		IsAdmin:           claims.IsAdmin,
		SessionGeneration: claims.SessionGeneration,
	}, nil
}

func loginAdmin(ctx context.Context, db *sql.DB, email, password string, metadata authRequestMetadata, now time.Time) (currentUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	emailIPDigest := authDigest(email + "\x00" + metadata.IPAddress)
	ipDigest := authDigest(metadata.IPAddress)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return currentUser{}, fmt.Errorf("%w: begin login", errAuthDependencyUnavailable)
	}
	defer tx.Rollback()
	if err := lockAuthRateLimitKeys(ctx, tx, emailIPDigest, ipDigest); err != nil {
		return currentUser{}, fmt.Errorf("%w: lock login rate limit", errAuthDependencyUnavailable)
	}
	emailIPCount, emailIPRetry, err := activeFailureCount(ctx, tx, "email_ip", emailIPDigest, now)
	if err != nil {
		return currentUser{}, fmt.Errorf("%w: read login rate limit", errAuthDependencyUnavailable)
	}
	ipCount, ipRetry, err := activeFailureCount(ctx, tx, "ip", ipDigest, now)
	if err != nil {
		return currentUser{}, fmt.Errorf("%w: read login rate limit", errAuthDependencyUnavailable)
	}
	if emailIPCount >= authEmailIPLimit || ipCount >= authIPLimit {
		retryAfter := emailIPRetry
		if ipRetry > retryAfter {
			retryAfter = ipRetry
		}
		if retryAfter < 1 {
			retryAfter = 1
		}
		if err := insertAuthAudit(ctx, tx, "auth.login", "failure", nil, metadata, now); err != nil {
			return currentUser{}, fmt.Errorf("%w: record rate limit audit", errAuthDependencyUnavailable)
		}
		if err := tx.Commit(); err != nil {
			return currentUser{}, fmt.Errorf("%w: commit rate limit audit", errAuthDependencyUnavailable)
		}
		return currentUser{}, authRateLimitError{RetryAfter: retryAfter}
	}

	var user currentUser
	var passwordHash string
	err = tx.QueryRowContext(ctx, `
			SELECT id::text, email, password_hash, display_name, is_admin, session_generation
			FROM users
			WHERE lower(email) = $1
		`, email).Scan(&user.ID, &user.Email, &passwordHash, &user.DisplayName, &user.IsAdmin, &user.SessionGeneration)
	userExists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return currentUser{}, fmt.Errorf("%w: read administrator", errAuthDependencyUnavailable)
	}
	if !userExists {
		passwordHash = dummyPasswordHash
	}
	passwordValid := checkPassword(passwordHash, password)
	valid := email != "" && password != "" && userExists && user.IsAdmin && passwordValid
	if !valid {
		if err := insertLoginFailure(ctx, tx, "email_ip", emailIPDigest, now); err != nil {
			return currentUser{}, fmt.Errorf("%w: record email login failure", errAuthDependencyUnavailable)
		}
		if err := insertLoginFailure(ctx, tx, "ip", ipDigest, now); err != nil {
			return currentUser{}, fmt.Errorf("%w: record IP login failure", errAuthDependencyUnavailable)
		}
		var actorID *string
		if userExists {
			actorID = &user.ID
		}
		if err := insertAuthAudit(ctx, tx, "auth.login", "failure", actorID, metadata, now); err != nil {
			return currentUser{}, fmt.Errorf("%w: record login audit", errAuthDependencyUnavailable)
		}
		if err := tx.Commit(); err != nil {
			return currentUser{}, fmt.Errorf("%w: commit login failure", errAuthDependencyUnavailable)
		}
		return currentUser{}, errInvalidCredentials
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_login_failure_events WHERE scope = 'email_ip' AND key_digest = $1`, emailIPDigest); err != nil {
		return currentUser{}, fmt.Errorf("%w: clear login failures", errAuthDependencyUnavailable)
	}
	if err := insertAuthAudit(ctx, tx, "auth.login", "success", &user.ID, metadata, now); err != nil {
		return currentUser{}, fmt.Errorf("%w: record login audit", errAuthDependencyUnavailable)
	}
	if err := tx.Commit(); err != nil {
		return currentUser{}, fmt.Errorf("%w: commit login", errAuthDependencyUnavailable)
	}
	user.Name = user.DisplayName
	user.Nickname = user.DisplayName
	return user, nil
}

type authSQLExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type authSQLQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func authDigest(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}

func lockAuthRateLimitKeys(ctx context.Context, tx *sql.Tx, digests ...[]byte) error {
	keys := make([]int64, 0, len(digests))
	for _, digest := range digests {
		keys = append(keys, int64(binary.BigEndian.Uint64(digest[:8])))
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for index, key := range keys {
		if index > 0 && key == keys[index-1] {
			continue
		}
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
			return err
		}
	}
	return nil
}

func activeFailureCount(ctx context.Context, query authSQLQuerier, scope string, digest []byte, now time.Time) (int, int, error) {
	var count int
	var firstExpiry sql.NullTime
	err := query.QueryRowContext(ctx, `
		SELECT count(*), min(expires_at)
		FROM auth_login_failure_events
		WHERE scope = $1 AND key_digest = $2 AND expires_at > $3
	`, scope, digest, now).Scan(&count, &firstExpiry)
	if err != nil {
		return 0, 0, err
	}
	retryAfter := 0
	if firstExpiry.Valid {
		delay := firstExpiry.Time.Sub(now)
		if delay > 0 {
			retryAfter = int((delay + time.Second - 1) / time.Second)
		}
	}
	return count, retryAfter, nil
}

func insertLoginFailure(ctx context.Context, exec authSQLExecutor, scope string, digest []byte, now time.Time) error {
	id, err := randomUUID()
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `
		INSERT INTO auth_login_failure_events (id, scope, key_digest, occurred_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, id, scope, digest, now, now.Add(authRateLimitWindow))
	return err
}

func insertAuthAudit(ctx context.Context, exec authSQLExecutor, action, result string, actorID *string, metadata authRequestMetadata, now time.Time) error {
	id, err := randomUUID()
	if err != nil {
		return err
	}
	auditMetadata := map[string]any{}
	if metadata.ClientRequestID != "" {
		auditMetadata["client_request_id"] = metadata.ClientRequestID
	}
	encodedMetadata, err := json.Marshal(auditMetadata)
	if err != nil {
		return err
	}
	var ipAddress any
	if candidate := strings.TrimSpace(metadata.IPAddress); net.ParseIP(candidate) != nil {
		ipAddress = candidate
	}
	_, err = exec.ExecContext(ctx, `
		INSERT INTO audit_logs (
			id, actor_type, actor_id, action, result, resource_type,
			resource_id, ip_address, request_id, metadata, occurred_at
		) VALUES ($1, 'admin', $2, $3, $4, 'auth_session', $2, $5, $6, $7, $8)
	`, id, actorID, action, result, ipAddress, nullableString(metadata.RequestID), encodedMetadata, now)
	return err
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func userFromRequest(r *http.Request) (currentUser, bool) {
	user, ok := r.Context().Value(currentUserContextKey{}).(currentUser)
	return user, ok
}

type currentUserContextKey struct{}

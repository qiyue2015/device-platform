package main

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	minJWTSecretLength     = 32
	tokenTTL               = 24 * time.Hour
	defaultMemoryJWTSecret = "0123456789abcdef0123456789abcdef"
)

type currentUser struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Nickname    string `json:"nickname"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
}

type authenticator interface {
	Login(ctx context.Context, email, password string) (currentUser, error)
	IssueToken(user currentUser) (string, error)
	ParseToken(token string) (currentUser, error)
}

type dbAuthenticator struct {
	db     *sql.DB
	secret string
	now    func() time.Time
}

func newDBAuthenticator(db *sql.DB, secret string) dbAuthenticator {
	return dbAuthenticator{db: db, secret: secret, now: time.Now}
}

func (a dbAuthenticator) Login(ctx context.Context, email, password string) (currentUser, error) {
	return loginAdmin(ctx, a.db, email, password)
}

func (a dbAuthenticator) IssueToken(user currentUser) (string, error) {
	return createJWT(user, a.secret, a.now().UTC())
}

func (a dbAuthenticator) ParseToken(token string) (currentUser, error) {
	return parseJWT(token, a.secret, a.now().UTC())
}

type memoryAuthenticator struct {
	user         currentUser
	passwordHash string
	secret       string
	now          func() time.Time
}

func newMemoryAuthenticator(email, displayName, password, secret string) (memoryAuthenticator, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return memoryAuthenticator{}, err
	}
	if displayName == "" {
		displayName = "Test Admin"
	}
	return memoryAuthenticator{
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

func (a memoryAuthenticator) Login(_ context.Context, email, password string) (currentUser, error) {
	if strings.EqualFold(strings.TrimSpace(email), a.user.Email) && checkPassword(a.passwordHash, password) {
		return a.user, nil
	}
	return currentUser{}, sql.ErrNoRows
}

func (a memoryAuthenticator) IssueToken(user currentUser) (string, error) {
	return createJWT(user, a.secret, a.now().UTC())
}

func (a memoryAuthenticator) ParseToken(token string) (currentUser, error) {
	return parseJWT(token, a.secret, a.now().UTC())
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

func generateSecretHex(byteLength int) (string, error) {
	bytes := make([]byte, byteLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func createJWT(user currentUser, secret string, now time.Time) (string, error) {
	if len(secret) < minJWTSecretLength {
		return "", fmt.Errorf("jwt secret is too short")
	}
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims := map[string]any{
		"sub":      user.ID,
		"email":    user.Email,
		"name":     user.DisplayName,
		"is_admin": user.IsAdmin,
		"iat":      now.Unix(),
		"exp":      now.Add(tokenTTL).Unix(),
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
	var claims struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		IsAdmin bool   `json:"is_admin"`
		Expires int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return currentUser{}, errors.New("invalid token claims")
	}
	if claims.Subject == "" || claims.Email == "" || !claims.IsAdmin {
		return currentUser{}, errors.New("invalid token user")
	}
	if claims.Expires <= now.Unix() {
		return currentUser{}, errors.New("token expired")
	}
	return currentUser{
		ID:          claims.Subject,
		Name:        claims.Name,
		Nickname:    claims.Name,
		Email:       claims.Email,
		DisplayName: claims.Name,
		IsAdmin:     claims.IsAdmin,
	}, nil
}

func loginAdmin(ctx context.Context, db *sql.DB, email, password string) (currentUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return currentUser{}, sql.ErrNoRows
	}
	var user currentUser
	var passwordHash string
	err := db.QueryRowContext(ctx, `
		SELECT id::text, email, password_hash, display_name, is_admin
		FROM users
		WHERE lower(email) = $1
	`, email).Scan(&user.ID, &user.Email, &passwordHash, &user.DisplayName, &user.IsAdmin)
	if err != nil {
		return currentUser{}, err
	}
	if !user.IsAdmin || !checkPassword(passwordHash, password) {
		return currentUser{}, sql.ErrNoRows
	}
	user.Name = user.DisplayName
	user.Nickname = user.DisplayName
	return user, nil
}

func userFromRequest(r *http.Request) (currentUser, bool) {
	user, ok := r.Context().Value(currentUserContextKey{}).(currentUser)
	return user, ok
}

type currentUserContextKey struct{}

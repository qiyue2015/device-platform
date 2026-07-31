package projectservice

import (
	"errors"
	"io"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

var (
	ErrInvalidRequest          = errors.New("invalid project request")
	ErrProjectNotFound         = errors.New("project not found")
	ErrWebhookNotConfigured    = errors.New("project webhook not configured")
	ErrWebhookSecretNotFound   = errors.New("webhook secret version not found")
	ErrAuthenticationFailed    = errors.New("project authentication failed")
	ErrSourceIPNotAllowed      = errors.New("source IP not allowed")
	ErrStoredWhitelistInvalid  = errors.New("stored Project IP whitelist is invalid")
	ErrCredentialGeneration    = errors.New("credential generation failed")
	ErrEncryptionConfiguration = errors.New("webhook encryption configuration invalid")
	ErrWebhookSecretDecryption = errors.New("webhook secret decryption failed")
)

type Clock interface {
	Now() time.Time
}

type Config struct {
	EncryptionKeys             map[int][]byte
	ActiveEncryptionKeyVersion int
	Random                     io.Reader
	Clock                      Clock
}

// RequestMetadata carries transport-independent facts into durable Audit rows.
type RequestMetadata struct {
	ActorType domain.ActorType
	ActorID   string
	IPAddress string
	RequestID string
}

// Project deliberately excludes every credential digest and encrypted secret field.
type Project struct {
	ID                string
	Name              string
	WebhookURL        *string
	WebhookConfigured bool
	IPWhitelist       []string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type CreateRequest struct {
	Name        string
	WebhookURL  *string
	IPWhitelist []string
}

type UpdateRequest struct {
	Name          *string
	WebhookURLSet bool
	WebhookURL    *string
	IPWhitelist   *[]string
}

type CredentialResult struct {
	Project       Project
	APIKey        string
	WebhookSecret string
}

type UpdateResult struct {
	Project       Project
	WebhookSecret string
}

type ListRequest struct {
	Name     *string
	Page     int
	PageSize int
}

type ListResult struct {
	Items    []Project
	Page     int
	PageSize int
	Total    int64
}

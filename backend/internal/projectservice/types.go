package projectservice

import (
	"errors"
	"io"
	"time"

	"github.com/qiyue2015/device-platform/internal/access"
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
	ErrForbidden               = errors.New("project operation forbidden")
	ErrManagerNotFound         = errors.New("project manager not found")
	ErrManagerInactive         = errors.New("project manager is inactive")
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
	ActorUserID string
	IPAddress   string
	RequestID   string
}

type Manager struct {
	ID          string            `json:"id"`
	Email       string            `json:"email"`
	DisplayName string            `json:"display_name"`
	Status      domain.UserStatus `json:"status"`
}

// Project deliberately excludes every credential digest and encrypted secret field.
type Project struct {
	ID                             string
	Name                           string
	ManagerUserID                  string
	Manager                        Manager
	SensitiveConfigurationIncluded bool
	WebhookURL                     *string
	WebhookConfigured              bool
	IPWhitelist                    []string
	CreatedAt                      time.Time
	UpdatedAt                      time.Time
}

type CreateRequest struct {
	Name          string
	ManagerUserID string
	WebhookURL    *string
	IPWhitelist   []string
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
	Name          *string
	ManagerUserID *string
	Page          int
	PageSize      int
}

type TransferRequest struct {
	ManagerUserID string
}

type Scope = access.Scope

type ListResult struct {
	Items    []Project
	Page     int
	PageSize int
	Total    int64
}

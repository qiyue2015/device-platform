package projectservice

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/qiyue2015/device-platform/internal/access"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const (
	apiKeyPrefix        = "dp_"
	webhookSecretPrefix = "whsec_"
)

var errTransferRetry = errors.New("Project manager changed while acquiring locks")

type Service struct {
	store                      repository.ProjectStore
	keys                       map[int][]byte
	activeEncryptionKeyVersion int
	random                     io.Reader
	clock                      Clock
}

func New(store repository.ProjectStore, config Config) (*Service, error) {
	if store == nil || config.ActiveEncryptionKeyVersion < 1 {
		return nil, ErrEncryptionConfiguration
	}
	keys := make(map[int][]byte, len(config.EncryptionKeys))
	for version, key := range config.EncryptionKeys {
		if version < 1 || len(key) != 32 {
			return nil, ErrEncryptionConfiguration
		}
		keys[version] = slices.Clone(key)
	}
	if len(keys[config.ActiveEncryptionKeyVersion]) != 32 {
		return nil, ErrEncryptionConfiguration
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	return &Service{
		store:                      store,
		keys:                       keys,
		activeEncryptionKeyVersion: config.ActiveEncryptionKeyVersion,
		random:                     config.Random,
		clock:                      config.Clock,
	}, nil
}

func (s *Service) Create(ctx context.Context, scope Scope, request CreateRequest, metadata RequestMetadata) (CredentialResult, error) {
	if err := validateHumanScope(scope); err != nil {
		return CredentialResult{}, err
	}
	if !scope.IsSuperAdmin() {
		return CredentialResult{}, ErrForbidden
	}
	if !validUUID(request.ManagerUserID) {
		return CredentialResult{}, ErrInvalidRequest
	}
	name, err := normalizeName(request.Name)
	if err != nil {
		return CredentialResult{}, err
	}
	webhookURL, err := normalizeWebhookURL(request.WebhookURL)
	if err != nil {
		return CredentialResult{}, err
	}
	whitelist, err := normalizeWhitelist(request.IPWhitelist)
	if err != nil {
		return CredentialResult{}, err
	}
	metadata, err = validateRequestMetadata(metadata)
	if err != nil {
		return CredentialResult{}, err
	}
	if scope.UserID != "" && scope.UserID != metadata.ActorUserID {
		return CredentialResult{}, ErrInvalidRequest
	}
	projectID, err := randomUUID(s.random)
	if err != nil {
		return CredentialResult{}, err
	}
	apiKey, err := generateCredential(s.random, apiKeyPrefix)
	if err != nil {
		return CredentialResult{}, err
	}
	digest := apiKeyDigest(apiKey)
	var webhookSecret string
	var encryptedSecret, nonce []byte
	if webhookURL != nil {
		webhookSecret, err = generateCredential(s.random, webhookSecretPrefix)
		if err != nil {
			return CredentialResult{}, err
		}
		encryptedSecret, nonce, err = encryptSecret(s.random, s.keys[s.activeEncryptionKeyVersion], projectID, 1, s.activeEncryptionKeyVersion, webhookSecret)
		if err != nil {
			return CredentialResult{}, err
		}
	}

	now := s.clock.Now().UTC()
	var created domain.Project
	err = s.store.TransactProject(ctx, func(tx repository.ProjectTx) error {
		manager, managerErr := tx.Users().GetForUpdate(ctx, request.ManagerUserID)
		if errors.Is(managerErr, sql.ErrNoRows) {
			return ErrManagerNotFound
		}
		if managerErr != nil {
			return managerErr
		}
		if manager.Status != domain.UserStatusActive {
			return ErrManagerInactive
		}
		project := domain.Project{
			ID: projectID, Name: name, ManagerUserID: manager.ID, Manager: manager,
			APIKeyDigest: digest[:], IPWhitelist: whitelist, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Projects().Create(ctx, project); err != nil {
			return err
		}
		if webhookURL != nil {
			secretVersion := 1
			if err := tx.Projects().CreateWebhookSecretVersion(ctx, domain.WebhookSecretVersion{
				ProjectID:            projectID,
				Version:              secretVersion,
				Ciphertext:           encryptedSecret,
				Nonce:                nonce,
				EncryptionKeyVersion: s.activeEncryptionKeyVersion,
				CreatedAt:            now,
			}); err != nil {
				return err
			}
			if err := tx.Projects().SetWebhookConfiguration(ctx, projectID, webhookURL, 1, &secretVersion); err != nil {
				return err
			}
		}
		created, err = tx.Projects().Get(ctx, projectID)
		if err != nil {
			return err
		}
		return s.createAudit(ctx, tx, metadata, projectID, "project.created", map[string]any{
			"manager_user_id":    manager.ID,
			"webhook_configured": webhookURL != nil,
			"ip_whitelist_count": len(whitelist),
		})
	})
	if err != nil {
		return CredentialResult{}, err
	}
	return CredentialResult{Project: safeProject(created, true), APIKey: apiKey, WebhookSecret: webhookSecret}, nil
}

func (s *Service) List(ctx context.Context, scope Scope, request ListRequest) (ListResult, error) {
	if err := validateHumanScope(scope); err != nil {
		return ListResult{}, err
	}
	page, pageSize := request.Page, request.PageSize
	if page == 0 {
		page = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if page < 1 || pageSize < 1 || pageSize > 100 || page-1 > math.MaxInt/pageSize {
		return ListResult{}, fmt.Errorf("%w: pagination is invalid", ErrInvalidRequest)
	}
	if request.Name != nil && (!utf8.ValidString(*request.Name) || *request.Name == "" || utf8.RuneCountInString(*request.Name) > 120) {
		return ListResult{}, fmt.Errorf("%w: name filter is invalid", ErrInvalidRequest)
	}
	managerUserID := request.ManagerUserID
	if managerUserID != nil && !validUUID(*managerUserID) {
		return ListResult{}, fmt.Errorf("%w: manager_user_id filter is invalid", ErrInvalidRequest)
	}
	if scope.Kind == access.ScopeUser {
		if managerUserID != nil && *managerUserID != scope.UserID {
			return ListResult{}, ErrForbidden
		}
		value := scope.UserID
		managerUserID = &value
	}
	items, total, err := s.store.Projects().List(ctx, repository.ListProjectsRequest{
		Name: request.Name, ManagerUserID: managerUserID, Limit: pageSize, Offset: (page - 1) * pageSize,
	})
	if err != nil {
		return ListResult{}, err
	}
	result := make([]Project, 0, len(items))
	for _, item := range items {
		result = append(result, safeProject(item, scope.IsSuperAdmin()))
	}
	return ListResult{Items: result, Page: page, PageSize: pageSize, Total: total}, nil
}

func (s *Service) Get(ctx context.Context, scope Scope, projectID string) (Project, error) {
	if validateHumanScope(scope) != nil || !validUUID(projectID) {
		return Project{}, fmt.Errorf("%w: project_id is invalid", ErrInvalidRequest)
	}
	project, err := s.store.Projects().Get(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrProjectNotFound
	}
	if err != nil {
		return Project{}, err
	}
	if !scope.CanAccessProject(project) {
		return Project{}, ErrProjectNotFound
	}
	return safeProject(project, scope.IsSuperAdmin()), nil
}

func (s *Service) Update(ctx context.Context, scope Scope, projectID string, request UpdateRequest, metadata RequestMetadata) (UpdateResult, error) {
	if validateHumanScope(scope) != nil || !validUUID(projectID) || (request.Name == nil && !request.WebhookURLSet && request.IPWhitelist == nil) || (!request.WebhookURLSet && request.WebhookURL != nil) {
		return UpdateResult{}, ErrInvalidRequest
	}
	var name *string
	var err error
	if request.Name != nil {
		value, normalizeErr := normalizeName(*request.Name)
		if normalizeErr != nil {
			return UpdateResult{}, normalizeErr
		}
		name = &value
	}
	var webhookURL *string
	if request.WebhookURLSet {
		webhookURL, err = normalizeWebhookURL(request.WebhookURL)
		if err != nil {
			return UpdateResult{}, err
		}
	}
	var whitelist *[]string
	if request.IPWhitelist != nil {
		value, normalizeErr := normalizeWhitelist(*request.IPWhitelist)
		if normalizeErr != nil {
			return UpdateResult{}, normalizeErr
		}
		whitelist = &value
	}
	metadata, err = validateRequestMetadata(metadata)
	if err != nil {
		return UpdateResult{}, err
	}
	if scope.UserID != "" && scope.UserID != metadata.ActorUserID {
		return UpdateResult{}, ErrInvalidRequest
	}

	var updated domain.Project
	var oneTimeSecret string
	err = s.store.TransactProject(ctx, func(tx repository.ProjectTx) error {
		current, lockErr := tx.Projects().GetForUpdate(ctx, projectID)
		if errors.Is(lockErr, sql.ErrNoRows) {
			return ErrProjectNotFound
		}
		if lockErr != nil {
			return lockErr
		}
		if !scope.CanAccessProject(current) {
			return ErrProjectNotFound
		}
		if !scope.IsSuperAdmin() && (request.WebhookURLSet || request.IPWhitelist != nil) {
			return ErrForbidden
		}
		fields := make([]string, 0, 3)
		if name != nil {
			fields = append(fields, "name")
			if current.Name != *name {
				if err := tx.Projects().Rename(ctx, projectID, *name); err != nil {
					return err
				}
			}
		}
		if whitelist != nil {
			fields = append(fields, "ip_whitelist")
			if !slices.Equal(current.IPWhitelist, *whitelist) {
				if err := tx.Projects().ReplaceIPWhitelist(ctx, projectID, *whitelist); err != nil {
					return err
				}
			}
		}
		if request.WebhookURLSet {
			fields = append(fields, "webhook_url")
			if !equalOptionalString(current.WebhookURL, webhookURL) {
				secretVersion := current.CurrentWebhookSecretVersion
				if webhookURL != nil && secretVersion == nil {
					oneTimeSecret, err = generateCredential(s.random, webhookSecretPrefix)
					if err != nil {
						return err
					}
					version := 1
					ciphertext, nonce, encryptErr := encryptSecret(s.random, s.keys[s.activeEncryptionKeyVersion], projectID, version, s.activeEncryptionKeyVersion, oneTimeSecret)
					if encryptErr != nil {
						return encryptErr
					}
					if err := tx.Projects().CreateWebhookSecretVersion(ctx, domain.WebhookSecretVersion{
						ProjectID: projectID, Version: version, Ciphertext: ciphertext, Nonce: nonce,
						EncryptionKeyVersion: s.activeEncryptionKeyVersion, CreatedAt: s.clock.Now().UTC(),
					}); err != nil {
						return err
					}
					secretVersion = &version
				}
				if err := tx.Projects().SetWebhookConfiguration(ctx, projectID, webhookURL, current.WebhookConfigVersion+1, secretVersion); err != nil {
					return err
				}
			}
		}
		updated, err = tx.Projects().Get(ctx, projectID)
		if err != nil {
			return err
		}
		return s.createAudit(ctx, tx, metadata, projectID, "project.updated", map[string]any{"fields": fields})
	})
	if err != nil {
		return UpdateResult{}, err
	}
	return UpdateResult{Project: safeProject(updated, scope.IsSuperAdmin()), WebhookSecret: oneTimeSecret}, nil
}

func (s *Service) RotateAPIKey(ctx context.Context, scope Scope, projectID string, metadata RequestMetadata) (CredentialResult, error) {
	if validateHumanScope(scope) != nil || !validUUID(projectID) {
		return CredentialResult{}, ErrInvalidRequest
	}
	metadata, err := validateRequestMetadata(metadata)
	if err != nil {
		return CredentialResult{}, err
	}
	if scope.UserID != "" && scope.UserID != metadata.ActorUserID {
		return CredentialResult{}, ErrInvalidRequest
	}
	apiKey, err := generateCredential(s.random, apiKeyPrefix)
	if err != nil {
		return CredentialResult{}, err
	}
	digest := apiKeyDigest(apiKey)
	var updated domain.Project
	err = s.store.TransactProject(ctx, func(tx repository.ProjectTx) error {
		current, lockErr := tx.Projects().GetForUpdate(ctx, projectID)
		if errors.Is(lockErr, sql.ErrNoRows) {
			return ErrProjectNotFound
		} else if lockErr != nil {
			return lockErr
		}
		if !scope.CanAccessProject(current) {
			return ErrProjectNotFound
		}
		if !scope.IsSuperAdmin() {
			return ErrForbidden
		}
		if err := tx.Projects().ReplaceAPIKeyDigest(ctx, projectID, digest[:]); err != nil {
			return err
		}
		updated, err = tx.Projects().Get(ctx, projectID)
		if err != nil {
			return err
		}
		return s.createAudit(ctx, tx, metadata, projectID, "project.api_key_rotated", map[string]any{})
	})
	if err != nil {
		return CredentialResult{}, err
	}
	return CredentialResult{Project: safeProject(updated, true), APIKey: apiKey}, nil
}

func (s *Service) RotateWebhookSecret(ctx context.Context, scope Scope, projectID string, metadata RequestMetadata) (CredentialResult, error) {
	if validateHumanScope(scope) != nil || !validUUID(projectID) {
		return CredentialResult{}, ErrInvalidRequest
	}
	metadata, err := validateRequestMetadata(metadata)
	if err != nil {
		return CredentialResult{}, err
	}
	if scope.UserID != "" && scope.UserID != metadata.ActorUserID {
		return CredentialResult{}, ErrInvalidRequest
	}
	var updated domain.Project
	var plaintext string
	err = s.store.TransactProject(ctx, func(tx repository.ProjectTx) error {
		current, lockErr := tx.Projects().GetForUpdate(ctx, projectID)
		if errors.Is(lockErr, sql.ErrNoRows) {
			return ErrProjectNotFound
		}
		if lockErr != nil {
			return lockErr
		}
		if !scope.CanAccessProject(current) {
			return ErrProjectNotFound
		}
		if !scope.IsSuperAdmin() {
			return ErrForbidden
		}
		if current.WebhookURL == nil || current.CurrentWebhookSecretVersion == nil {
			return ErrWebhookNotConfigured
		}
		plaintext, err = generateCredential(s.random, webhookSecretPrefix)
		if err != nil {
			return err
		}
		version := *current.CurrentWebhookSecretVersion + 1
		ciphertext, nonce, encryptErr := encryptSecret(s.random, s.keys[s.activeEncryptionKeyVersion], projectID, version, s.activeEncryptionKeyVersion, plaintext)
		if encryptErr != nil {
			return encryptErr
		}
		if err := tx.Projects().CreateWebhookSecretVersion(ctx, domain.WebhookSecretVersion{
			ProjectID: projectID, Version: version, Ciphertext: ciphertext, Nonce: nonce,
			EncryptionKeyVersion: s.activeEncryptionKeyVersion, CreatedAt: s.clock.Now().UTC(),
		}); err != nil {
			return err
		}
		if err := tx.Projects().SetWebhookConfiguration(ctx, projectID, current.WebhookURL, current.WebhookConfigVersion+1, &version); err != nil {
			return err
		}
		if err := tx.Projects().RetireWebhookSecretVersion(ctx, projectID, *current.CurrentWebhookSecretVersion); err != nil {
			return err
		}
		updated, err = tx.Projects().Get(ctx, projectID)
		if err != nil {
			return err
		}
		return s.createAudit(ctx, tx, metadata, projectID, "project.webhook_secret_rotated", map[string]any{
			"webhook_secret_version": version,
		})
	})
	if err != nil {
		return CredentialResult{}, err
	}
	return CredentialResult{Project: safeProject(updated, true), WebhookSecret: plaintext}, nil
}

func (s *Service) Transfer(ctx context.Context, scope Scope, projectID string, request TransferRequest, metadata RequestMetadata) (Project, error) {
	if validateHumanScope(scope) != nil || !validUUID(projectID) || !validUUID(request.ManagerUserID) {
		return Project{}, ErrInvalidRequest
	}
	if !scope.IsSuperAdmin() {
		visible, err := s.store.Projects().Get(ctx, projectID)
		if errors.Is(err, sql.ErrNoRows) || err == nil && !scope.CanAccessProject(visible) {
			return Project{}, ErrProjectNotFound
		}
		if err != nil {
			return Project{}, err
		}
		return Project{}, ErrForbidden
	}
	metadata, err := validateRequestMetadata(metadata)
	if err != nil || scope.UserID != "" && scope.UserID != metadata.ActorUserID {
		return Project{}, ErrInvalidRequest
	}
	observed, err := s.store.Projects().Get(ctx, projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrProjectNotFound
	}
	if err != nil {
		return Project{}, err
	}
	for attempt := 0; attempt < 4; attempt++ {
		var updated domain.Project
		err = s.store.TransactProject(ctx, func(tx repository.ProjectTx) error {
			userIDs := []string{observed.ManagerUserID, request.ManagerUserID}
			slices.Sort(userIDs)
			userIDs = slices.Compact(userIDs)
			var target domain.User
			for _, userID := range userIDs {
				locked, lockErr := tx.Users().GetForUpdate(ctx, userID)
				if errors.Is(lockErr, sql.ErrNoRows) && userID == request.ManagerUserID {
					return ErrManagerNotFound
				}
				if lockErr != nil {
					return lockErr
				}
				if userID == request.ManagerUserID {
					target = locked
				}
			}
			current, lockErr := tx.Projects().GetForUpdate(ctx, projectID)
			if errors.Is(lockErr, sql.ErrNoRows) {
				return ErrProjectNotFound
			}
			if lockErr != nil {
				return lockErr
			}
			if current.ManagerUserID != observed.ManagerUserID {
				return errTransferRetry
			}
			if target.Status != domain.UserStatusActive {
				return ErrManagerInactive
			}
			if current.ManagerUserID == target.ID {
				updated = current
				return nil
			}
			if setErr := tx.Projects().SetManager(ctx, projectID, target.ID); setErr != nil {
				return setErr
			}
			updated, lockErr = tx.Projects().Get(ctx, projectID)
			if lockErr != nil {
				return lockErr
			}
			return s.createAudit(ctx, tx, metadata, projectID, "project.transferred", map[string]any{
				"from_manager_user_id": current.ManagerUserID,
				"to_manager_user_id":   target.ID,
			})
		})
		if !errors.Is(err, errTransferRetry) {
			if err != nil {
				return Project{}, err
			}
			return safeProject(updated, true), nil
		}
		observed, err = s.store.Projects().Get(ctx, projectID)
		if errors.Is(err, sql.ErrNoRows) {
			return Project{}, ErrProjectNotFound
		}
		if err != nil {
			return Project{}, err
		}
	}
	return Project{}, errTransferRetry
}

// AuthenticateAPIKey uses only the direct peer address supplied by the server.
// Forwarded headers are intentionally outside this interface.
func (s *Service) AuthenticateAPIKey(ctx context.Context, apiKey, peerAddress string) (Project, error) {
	if !validCredential(apiKey, apiKeyPrefix) {
		return Project{}, ErrAuthenticationFailed
	}
	digest := apiKeyDigest(apiKey)
	project, err := s.store.Projects().GetByAPIKeyDigest(ctx, digest[:])
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrAuthenticationFailed
	}
	if err != nil {
		return Project{}, err
	}
	peer, err := parsePeerAddress(peerAddress)
	if err != nil {
		return Project{}, ErrSourceIPNotAllowed
	}
	allowed, err := whitelistAllows(project.IPWhitelist, peer)
	if err != nil {
		return Project{}, fmt.Errorf("%w: %v", ErrStoredWhitelistInvalid, err)
	}
	if !allowed {
		return Project{}, ErrSourceIPNotAllowed
	}
	return safeProject(project, false), nil
}

func (s *Service) ResolveWebhookSecret(ctx context.Context, projectID string, version int) (string, error) {
	if !validUUID(projectID) || version < 1 {
		return "", ErrInvalidRequest
	}
	secret, err := s.store.Projects().GetWebhookSecretVersion(ctx, projectID, version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrWebhookSecretNotFound
	}
	if err != nil {
		return "", err
	}
	key, exists := s.keys[secret.EncryptionKeyVersion]
	if !exists {
		return "", ErrWebhookSecretDecryption
	}
	return decryptSecret(key, secret.Ciphertext, secret.Nonce, projectID, secret.Version, secret.EncryptionKeyVersion)
}

func (s *Service) createAudit(ctx context.Context, tx repository.ProjectTx, metadata RequestMetadata, projectID, action string, fields map[string]any) error {
	id, err := randomUUID(s.random)
	if err != nil {
		return err
	}
	projectIDCopy := projectID
	resourceID := projectID
	return tx.Audits().Create(ctx, domain.AuditLog{
		ID: id, ActorType: domain.ActorTypeUser, ActorUserID: optionalString(metadata.ActorUserID),
		ProjectID: &projectIDCopy, Action: action, Result: domain.AuditResultSuccess,
		ResourceType: "project", ResourceID: &resourceID, IPAddress: optionalString(metadata.IPAddress),
		RequestID: optionalString(metadata.RequestID), Metadata: fields, OccurredAt: s.clock.Now().UTC(),
	})
}

func safeProject(project domain.Project, includeSensitive bool) Project {
	result := Project{
		ID: project.ID, Name: project.Name, ManagerUserID: project.ManagerUserID,
		Manager:                        Manager{ID: project.Manager.ID, Email: project.Manager.Email, DisplayName: project.Manager.DisplayName, Status: project.Manager.Status},
		SensitiveConfigurationIncluded: includeSensitive,
		CreatedAt:                      project.CreatedAt,
		UpdatedAt:                      project.UpdatedAt,
	}
	if includeSensitive {
		result.WebhookURL = cloneString(project.WebhookURL)
		result.WebhookConfigured = project.WebhookURL != nil
		result.IPWhitelist = slices.Clone(project.IPWhitelist)
	}
	return result
}

func validateHumanScope(scope Scope) error {
	if scope.Validate() != nil || !scope.IsHuman() || scope.UserID != "" && !validUUID(scope.UserID) {
		return ErrInvalidRequest
	}
	return nil
}

func validCredential(value, prefix string) bool {
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil && len(decoded) == sha256.Size
}

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

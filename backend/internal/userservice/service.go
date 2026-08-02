package userservice

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/mail"
	"net/netip"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

func New(store repository.UserStore, config Config) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidRequest)
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.Clock == nil {
		config.Clock = realClock{}
	}
	return &Service{store: store, random: config.Random, clock: config.Clock}, nil
}

func (s *Service) Create(ctx context.Context, scope Scope, request CreateRequest, metadata RequestMetadata) (User, error) {
	if err := requireSuperAdmin(scope); err != nil {
		return User{}, err
	}
	request, err := normalizeCreateRequest(request)
	if err != nil {
		return User{}, err
	}
	metadata, err = normalizeMetadata(metadata)
	if err != nil {
		return User{}, err
	}
	id, err := randomUUID(s.random)
	if err != nil {
		return User{}, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, fmt.Errorf("hash User password: %w", err)
	}
	now := s.clock.Now().UTC()
	created := domain.User{
		ID: id, Email: request.Email, PasswordHash: string(passwordHash), DisplayName: request.DisplayName,
		IsSuperAdmin: false, Status: domain.UserStatusActive, CreatedAt: now, UpdatedAt: now,
	}
	err = s.store.TransactUser(ctx, func(tx repository.UserTx) error {
		if createErr := tx.Users().Create(ctx, created); createErr != nil {
			return createErr
		}
		return createAudit(ctx, tx.Audits(), s.random, metadata, created.ID, "user.created", map[string]any{
			"status": created.Status,
		}, now)
	})
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return User{}, ErrEmailConflict
	}
	if err != nil {
		return User{}, err
	}
	return safeUser(created), nil
}

func (s *Service) List(ctx context.Context, scope Scope, request ListRequest) (ListResult, error) {
	if err := requireSuperAdmin(scope); err != nil {
		return ListResult{}, err
	}
	request, err := normalizeListRequest(request)
	if err != nil {
		return ListResult{}, err
	}
	items, total, err := s.store.Users().List(ctx, repository.ListUsersRequest{
		Email: request.Email, Status: request.Status, Limit: request.PageSize,
		Offset: (request.Page - 1) * request.PageSize,
	})
	if err != nil {
		return ListResult{}, err
	}
	mapped := make([]User, 0, len(items))
	for _, item := range items {
		mapped = append(mapped, safeUser(item))
	}
	return ListResult{Items: mapped, Page: request.Page, PageSize: request.PageSize, Total: total}, nil
}

func (s *Service) Get(ctx context.Context, scope Scope, userID string) (User, error) {
	if err := requireSuperAdmin(scope); err != nil {
		return User{}, err
	}
	if !validUUID(userID) {
		return User{}, ErrInvalidRequest
	}
	user, err := s.store.Users().Get(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return safeUser(user), nil
}

func (s *Service) UpdateStatus(ctx context.Context, scope Scope, userID string, request UpdateStatusRequest, metadata RequestMetadata) (User, error) {
	if err := requireSuperAdmin(scope); err != nil {
		return User{}, err
	}
	if !validUUID(userID) || request.Status != domain.UserStatusActive && request.Status != domain.UserStatusDisabled {
		return User{}, ErrInvalidRequest
	}
	metadata, err := normalizeMetadata(metadata)
	if err != nil {
		return User{}, err
	}
	var updated domain.User
	err = s.store.TransactUser(ctx, func(tx repository.UserTx) error {
		current, lockErr := tx.Users().GetForUpdate(ctx, userID)
		if errors.Is(lockErr, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		if lockErr != nil {
			return lockErr
		}
		if current.Status == request.Status {
			updated = current
			return nil
		}
		if current.IsSuperAdmin {
			return ErrSuperAdminImmutable
		}
		if request.Status == domain.UserStatusDisabled {
			managed, countErr := tx.Projects().CountByManager(ctx, current.ID)
			if countErr != nil {
				return countErr
			}
			if managed != 0 {
				return ErrUserHasManagedProjects
			}
		}
		if setErr := tx.Users().SetStatus(ctx, current.ID, request.Status, request.Status == domain.UserStatusDisabled); setErr != nil {
			return setErr
		}
		updated, lockErr = tx.Users().Get(ctx, current.ID)
		if lockErr != nil {
			return lockErr
		}
		return createAudit(ctx, tx.Audits(), s.random, metadata, current.ID, "user.status_changed", map[string]any{
			"from": current.Status,
			"to":   updated.Status,
		}, s.clock.Now().UTC())
	})
	if err != nil {
		return User{}, err
	}
	return safeUser(updated), nil
}

func requireSuperAdmin(scope Scope) error {
	if scope.Validate() != nil {
		return ErrInvalidRequest
	}
	if !scope.IsSuperAdmin() {
		return ErrForbidden
	}
	return nil
}

func normalizeCreateRequest(request CreateRequest) (CreateRequest, error) {
	request.Email = strings.ToLower(strings.TrimSpace(request.Email))
	request.DisplayName = strings.TrimSpace(request.DisplayName)
	address, err := mail.ParseAddress(request.Email)
	if err != nil || address.Address != request.Email || len(request.Email) > 254 ||
		!utf8.ValidString(request.DisplayName) || utf8.RuneCountInString(request.DisplayName) < 2 || utf8.RuneCountInString(request.DisplayName) > 80 ||
		!utf8.ValidString(request.Password) || len([]byte(request.Password)) < 8 || len([]byte(request.Password)) > 128 {
		return CreateRequest{}, ErrInvalidRequest
	}
	return request, nil
}

func normalizeListRequest(request ListRequest) (ListRequest, error) {
	if request.Page == 0 {
		request.Page = 1
	}
	if request.PageSize == 0 {
		request.PageSize = 20
	}
	if request.Page < 1 || request.PageSize < 1 || request.PageSize > 100 || request.Page-1 > math.MaxInt/request.PageSize {
		return ListRequest{}, ErrInvalidRequest
	}
	if request.Email != nil {
		value := strings.ToLower(strings.TrimSpace(*request.Email))
		address, err := mail.ParseAddress(value)
		if err != nil || address.Address != value || len(value) > 254 {
			return ListRequest{}, ErrInvalidRequest
		}
		request.Email = &value
	}
	if request.Status != nil && *request.Status != domain.UserStatusActive && *request.Status != domain.UserStatusDisabled {
		return ListRequest{}, ErrInvalidRequest
	}
	return request, nil
}

func normalizeMetadata(metadata RequestMetadata) (RequestMetadata, error) {
	metadata.ActorUserID = strings.TrimSpace(metadata.ActorUserID)
	metadata.RequestID = strings.TrimSpace(metadata.RequestID)
	metadata.IPAddress = strings.TrimSpace(metadata.IPAddress)
	if !validUUID(metadata.ActorUserID) || metadata.RequestID == "" || len(metadata.RequestID) > 255 {
		return RequestMetadata{}, ErrInvalidRequest
	}
	if metadata.IPAddress != "" {
		address, err := netip.ParseAddr(metadata.IPAddress)
		if err != nil || address.Zone() != "" {
			return RequestMetadata{}, ErrInvalidRequest
		}
		metadata.IPAddress = address.Unmap().String()
	}
	return metadata, nil
}

func createAudit(ctx context.Context, audits repository.AuditRepository, random io.Reader, metadata RequestMetadata, resourceID, action string, fields map[string]any, occurredAt time.Time) error {
	id, err := randomUUID(random)
	if err != nil {
		return err
	}
	actorUserID := metadata.ActorUserID
	resourceIDCopy := resourceID
	return audits.Create(ctx, domain.AuditLog{
		ID: id, ActorType: domain.ActorTypeUser, ActorUserID: &actorUserID,
		Action: action, Result: domain.AuditResultSuccess, ResourceType: "user", ResourceID: &resourceIDCopy,
		IPAddress: optionalString(metadata.IPAddress), RequestID: optionalString(metadata.RequestID),
		Metadata: fields, OccurredAt: occurredAt,
	})
}

func safeUser(user domain.User) User {
	return User{
		ID: user.ID, Email: user.Email, DisplayName: user.DisplayName, IsSuperAdmin: user.IsSuperAdmin,
		Status: user.Status, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt,
	}
}

func randomUUID(reader io.Reader) (string, error) {
	var value [16]byte
	if _, err := io.ReadFull(reader, value[:]); err != nil {
		return "", fmt.Errorf("%w: %v", ErrIdentifierGeneration, err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func validUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' || value != strings.ToLower(value) {
		return false
	}
	decoded, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil && len(decoded) == 16
}

func optionalString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	copy := value
	return &copy
}

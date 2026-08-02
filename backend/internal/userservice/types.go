package userservice

import (
	"errors"
	"io"
	"time"

	"github.com/qiyue2015/device-platform/internal/access"
	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

var (
	ErrInvalidRequest         = errors.New("invalid User request")
	ErrForbidden              = errors.New("User operation forbidden")
	ErrUserNotFound           = errors.New("User not found")
	ErrEmailConflict          = errors.New("User email conflict")
	ErrSuperAdminImmutable    = errors.New("super administrator is immutable")
	ErrUserHasManagedProjects = errors.New("User still manages Projects")
	ErrIdentifierGeneration   = errors.New("User identifier generation failed")
)

type Clock interface {
	Now() time.Time
}

type Config struct {
	Random io.Reader
	Clock  Clock
}

type RequestMetadata struct {
	ActorUserID string
	IPAddress   string
	RequestID   string
}

type User struct {
	ID           string            `json:"id"`
	Email        string            `json:"email"`
	DisplayName  string            `json:"display_name"`
	IsSuperAdmin bool              `json:"is_super_admin"`
	Status       domain.UserStatus `json:"status"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type CreateRequest struct {
	Email       string
	DisplayName string
	Password    string
}

type ListRequest struct {
	Email    *string
	Status   *domain.UserStatus
	Page     int
	PageSize int
}

type ListResult struct {
	Items    []User
	Page     int
	PageSize int
	Total    int64
}

type UpdateStatusRequest struct {
	Status domain.UserStatus
}

type Service struct {
	store  repository.UserStore
	random io.Reader
	clock  Clock
}

type Scope = access.Scope

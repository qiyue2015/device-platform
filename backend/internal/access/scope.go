package access

import (
	"errors"
	"strings"

	"github.com/qiyue2015/device-platform/internal/domain"
)

var ErrInvalidScope = errors.New("invalid authorization scope")

type ScopeKind string

const (
	ScopeSuperAdmin ScopeKind = "super_admin"
	ScopeUser       ScopeKind = "user"
	ScopeProject    ScopeKind = "project"
)

type Scope struct {
	Kind      ScopeKind
	UserID    string
	ProjectID string
}

func SuperAdmin(userID string) Scope {
	return Scope{Kind: ScopeSuperAdmin, UserID: strings.TrimSpace(userID)}
}

func User(userID string) Scope {
	return Scope{Kind: ScopeUser, UserID: strings.TrimSpace(userID)}
}

func Project(projectID string) Scope {
	return Scope{Kind: ScopeProject, ProjectID: strings.TrimSpace(projectID)}
}

func (scope Scope) Validate() error {
	switch scope.Kind {
	case ScopeSuperAdmin, ScopeUser:
		if strings.TrimSpace(scope.UserID) == "" || strings.TrimSpace(scope.ProjectID) != "" {
			return ErrInvalidScope
		}
	case ScopeProject:
		if strings.TrimSpace(scope.UserID) != "" || strings.TrimSpace(scope.ProjectID) == "" {
			return ErrInvalidScope
		}
	default:
		return ErrInvalidScope
	}
	return nil
}

func (scope Scope) IsSuperAdmin() bool {
	return scope.Kind == ScopeSuperAdmin
}

func (scope Scope) IsHuman() bool {
	return scope.Kind == ScopeSuperAdmin || scope.Kind == ScopeUser
}

func (scope Scope) ManagerUserID() *string {
	if scope.Kind != ScopeUser {
		return nil
	}
	value := scope.UserID
	return &value
}

func (scope Scope) CanAccessProject(project domain.Project) bool {
	switch scope.Kind {
	case ScopeSuperAdmin:
		return true
	case ScopeUser:
		return project.ManagerUserID == scope.UserID
	case ScopeProject:
		return project.ID == scope.ProjectID
	default:
		return false
	}
}

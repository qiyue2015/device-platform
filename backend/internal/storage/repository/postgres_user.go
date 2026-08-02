package repository

import (
	"context"
	"database/sql"

	"github.com/qiyue2015/device-platform/internal/domain"
)

type postgresUserRepository struct {
	exec postgresExecutor
}

func (r *postgresUserRepository) Get(ctx context.Context, id string) (domain.User, error) {
	return scanUser(r.exec.QueryRowContext(ctx, userSelect+` WHERE id = $1`, id))
}

func (r *postgresUserRepository) GetByEmail(ctx context.Context, normalizedEmail string) (domain.User, error) {
	return scanUser(r.exec.QueryRowContext(ctx, userSelect+` WHERE lower(email) = $1`, normalizedEmail))
}

func (r *postgresUserRepository) List(ctx context.Context, request ListUsersRequest) ([]domain.User, int64, error) {
	if request.Limit < 1 || request.Limit > 100 || request.Offset < 0 {
		return nil, 0, ErrInvalidRepositoryRequest
	}
	const where = `
		WHERE ($1::text IS NULL OR email = $1)
		  AND ($2::text IS NULL OR status = $2)`
	arguments := []any{nullableString(request.Email), nullableUserStatus(request.Status)}
	var total int64
	if err := r.exec.QueryRowContext(ctx, `SELECT count(*) FROM users`+where, arguments...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.exec.QueryContext(ctx, userSelect+where+`
		ORDER BY created_at DESC, id DESC
		LIMIT $3 OFFSET $4
	`, append(arguments, request.Limit, request.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]domain.User, 0, request.Limit)
	for rows.Next() {
		item, scanErr := scanUser(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *postgresUserRepository) Create(ctx context.Context, user domain.User) error {
	_, err := r.exec.ExecContext(ctx, `
		INSERT INTO users (
			id, email, password_hash, display_name, is_super_admin,
			status, session_generation, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, user.ID, user.Email, user.PasswordHash, user.DisplayName, user.IsSuperAdmin,
		user.Status, user.SessionGeneration, user.CreatedAt, user.UpdatedAt)
	return err
}

func (r *postgresUserRepository) GetForUpdate(ctx context.Context, id string) (domain.User, error) {
	return scanUser(r.exec.QueryRowContext(ctx, userSelect+` WHERE id = $1 FOR UPDATE`, id))
}

func (r *postgresUserRepository) SetStatus(ctx context.Context, id string, status domain.UserStatus, invalidateSessions bool) error {
	_, err := r.exec.ExecContext(ctx, `
		UPDATE users
		SET status = $2,
			session_generation = session_generation + CASE WHEN $3 THEN 1 ELSE 0 END,
			updated_at = now()
		WHERE id = $1
	`, id, status, invalidateSessions)
	return err
}

func (r *postgresUserRepository) IncrementSessionGeneration(ctx context.Context, id string, expected int64) (int64, bool, error) {
	var next int64
	err := r.exec.QueryRowContext(ctx, `
		UPDATE users
		SET session_generation = session_generation + 1, updated_at = now()
		WHERE id = $1 AND session_generation = $2
		RETURNING session_generation
	`, id, expected).Scan(&next)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return next, true, nil
}

const userSelect = `
	SELECT id::text, email, password_hash, display_name, is_super_admin,
		status, session_generation, created_at, updated_at
	FROM users`

func scanUser(row rowScanner) (domain.User, error) {
	var user domain.User
	err := row.Scan(
		&user.ID,
		&user.Email,
		&user.PasswordHash,
		&user.DisplayName,
		&user.IsSuperAdmin,
		&user.Status,
		&user.SessionGeneration,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	return user, err
}

func nullableUserStatus(value *domain.UserStatus) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

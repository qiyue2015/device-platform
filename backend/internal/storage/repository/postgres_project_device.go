package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/domain"
)

type postgresExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// PostgresStore owns the PostgreSQL persistence boundary. Additional domain
// repositories join this type as their implementations are completed.
type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) Projects() ProjectQueries {
	return &postgresProjectRepository{exec: s.db}
}

func (s *PostgresStore) DeviceTypes() DeviceTypeQueries {
	return &postgresDeviceTypeRepository{exec: s.db}
}

func (s *PostgresStore) Devices() DeviceQueries {
	return &postgresDeviceRepository{exec: s.db}
}

func (s *PostgresStore) Commands() CommandQueries {
	return &postgresCommandRepository{exec: s.db}
}

func (s *PostgresStore) Events() EventQueries {
	return &postgresEventRepository{exec: s.db}
}

func (s *PostgresStore) Audits() AuditQueries {
	return &postgresAuditRepository{exec: s.db}
}

func (s *PostgresStore) Webhooks() WebhookQueries {
	return &postgresWebhookRepository{exec: s.db}
}

func (s *PostgresStore) Simulator() SimulatorQueries {
	return &postgresSimulatorRepository{exec: s.db}
}

// WithinTransaction exposes only repositories already implemented on
// PostgresTx. It will satisfy the complete Store contract once the remaining
// persistence domains are added.
func (s *PostgresStore) WithinTransaction(ctx context.Context, fn func(*PostgresTx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin repository transaction: %w", err)
	}
	defer tx.Rollback()
	if err := fn(&PostgresTx{tx: tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit repository transaction: %w", err)
	}
	return nil
}

func (s *PostgresStore) TransactProject(ctx context.Context, fn func(ProjectTx) error) error {
	return s.WithinTransaction(ctx, func(tx *PostgresTx) error {
		return fn(tx)
	})
}

func (s *PostgresStore) TransactDevice(ctx context.Context, fn func(DeviceTx) error) error {
	return s.WithinTransaction(ctx, func(tx *PostgresTx) error {
		return fn(tx)
	})
}

func (s *PostgresStore) TransactCommand(ctx context.Context, fn func(CommandTx) error) error {
	return s.WithinTransaction(ctx, func(tx *PostgresTx) error {
		return fn(tx)
	})
}

func (s *PostgresStore) TransactSimulator(ctx context.Context, fn func(SimulatorTx) error) error {
	return s.WithinTransaction(ctx, func(tx *PostgresTx) error {
		return fn(tx)
	})
}

func (s *PostgresStore) TransactWebhookAudit(ctx context.Context, fn func(WebhookAuditTx) error) error {
	return s.WithinTransaction(ctx, func(tx *PostgresTx) error {
		return fn(tx)
	})
}

type PostgresTx struct {
	tx *sql.Tx
}

func (tx *PostgresTx) Projects() ProjectRepository {
	return &postgresProjectRepository{exec: tx.tx}
}

func (tx *PostgresTx) DeviceTypes() DeviceTypeQueries {
	return &postgresDeviceTypeRepository{exec: tx.tx}
}

func (tx *PostgresTx) Devices() DeviceRepository {
	return &postgresDeviceRepository{exec: tx.tx}
}

func (tx *PostgresTx) Commands() CommandRepository {
	return &postgresCommandRepository{exec: tx.tx}
}

func (tx *PostgresTx) Results() CommandResultRepository {
	return &postgresCommandResultRepository{exec: tx.tx}
}

func (tx *PostgresTx) Messages() RawMessageRepository {
	return &postgresRawMessageRepository{exec: tx.tx}
}

func (tx *PostgresTx) Events() EventRepository {
	return &postgresEventRepository{exec: tx.tx}
}

func (tx *PostgresTx) Audits() AuditRepository {
	return &postgresAuditRepository{exec: tx.tx}
}

func (tx *PostgresTx) Webhooks() WebhookRepository {
	return &postgresWebhookRepository{exec: tx.tx}
}

func (tx *PostgresTx) Simulator() SimulatorRepository {
	return &postgresSimulatorRepository{exec: tx.tx}
}

type postgresProjectRepository struct {
	exec postgresExecutor
}

func (r *postgresProjectRepository) Get(ctx context.Context, id string) (domain.Project, error) {
	return scanProject(r.exec.QueryRowContext(ctx, projectSelect+` WHERE id = $1`, id))
}

func (r *postgresProjectRepository) GetByAPIKeyDigest(ctx context.Context, digest []byte) (domain.Project, error) {
	return scanProject(r.exec.QueryRowContext(ctx, projectSelect+` WHERE api_key_hash = $1`, digest))
}

func (r *postgresProjectRepository) GetWebhookSecretVersion(ctx context.Context, projectID string, version int) (domain.WebhookSecretVersion, error) {
	var secret domain.WebhookSecretVersion
	var retiredAt sql.NullTime
	err := r.exec.QueryRowContext(ctx, `
		SELECT project_id::text, version, ciphertext, nonce, encryption_key_version, created_at, retired_at
		FROM project_webhook_secrets
		WHERE project_id = $1 AND version = $2
	`, projectID, version).Scan(
		&secret.ProjectID,
		&secret.Version,
		&secret.Ciphertext,
		&secret.Nonce,
		&secret.EncryptionKeyVersion,
		&secret.CreatedAt,
		&retiredAt,
	)
	if err != nil {
		return domain.WebhookSecretVersion{}, err
	}
	if retiredAt.Valid {
		secret.RetiredAt = &retiredAt.Time
	}
	return secret, nil
}

func (r *postgresProjectRepository) List(ctx context.Context, request ListProjectsRequest) ([]domain.Project, int64, error) {
	if request.Limit < 1 || request.Limit > 100 || request.Offset < 0 {
		return nil, 0, ErrInvalidRepositoryRequest
	}
	var total int64
	if err := r.exec.QueryRowContext(ctx, `
		SELECT count(*)
		FROM projects
		WHERE ($1::text IS NULL OR name = $1)
	`, nullableString(request.Name)).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.exec.QueryContext(ctx, projectSelect+`
		WHERE ($1::text IS NULL OR name = $1)
		ORDER BY created_at DESC, id DESC
		LIMIT $2 OFFSET $3
	`, nullableString(request.Name), request.Limit, request.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]domain.Project, 0, request.Limit)
	for rows.Next() {
		item, err := scanProject(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *postgresProjectRepository) Create(ctx context.Context, project domain.Project) error {
	_, err := r.exec.ExecContext(ctx, `
		INSERT INTO projects (
			id, name, api_key_hash, webhook_url, webhook_config_version,
			current_webhook_secret_version, ip_whitelist, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`,
		project.ID,
		project.Name,
		project.APIKeyDigest,
		nullableString(project.WebhookURL),
		project.WebhookConfigVersion,
		nullableInt(project.CurrentWebhookSecretVersion),
		pq.Array(project.IPWhitelist),
		project.CreatedAt,
		project.UpdatedAt,
	)
	return err
}

func (r *postgresProjectRepository) GetForUpdate(ctx context.Context, id string) (domain.Project, error) {
	return scanProject(r.exec.QueryRowContext(ctx, projectSelect+` WHERE id = $1 FOR UPDATE`, id))
}

func (r *postgresProjectRepository) Rename(ctx context.Context, id, name string) error {
	_, err := r.exec.ExecContext(ctx, `UPDATE projects SET name = $2, updated_at = now() WHERE id = $1`, id, name)
	return err
}

func (r *postgresProjectRepository) ReplaceIPWhitelist(ctx context.Context, id string, whitelist []string) error {
	_, err := r.exec.ExecContext(ctx, `UPDATE projects SET ip_whitelist = $2, updated_at = now() WHERE id = $1`, id, pq.Array(whitelist))
	return err
}

func (r *postgresProjectRepository) SetWebhookConfiguration(ctx context.Context, id string, webhookURL *string, configVersion int64, secretVersion *int) error {
	_, err := r.exec.ExecContext(ctx, `
		UPDATE projects
		SET webhook_url = $2, webhook_config_version = $3,
			current_webhook_secret_version = $4, updated_at = now()
		WHERE id = $1
	`, id, nullableString(webhookURL), configVersion, nullableInt(secretVersion))
	return err
}

func (r *postgresProjectRepository) ReplaceAPIKeyDigest(ctx context.Context, id string, digest []byte) error {
	_, err := r.exec.ExecContext(ctx, `UPDATE projects SET api_key_hash = $2, updated_at = now() WHERE id = $1`, id, digest)
	return err
}

func (r *postgresProjectRepository) CreateWebhookSecretVersion(ctx context.Context, secret domain.WebhookSecretVersion) error {
	_, err := r.exec.ExecContext(ctx, `
		INSERT INTO project_webhook_secrets (
			project_id, version, ciphertext, nonce, encryption_key_version, created_at, retired_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`,
		secret.ProjectID,
		secret.Version,
		secret.Ciphertext,
		secret.Nonce,
		secret.EncryptionKeyVersion,
		secret.CreatedAt,
		nullableTime(secret.RetiredAt),
	)
	return err
}

func (r *postgresProjectRepository) RetireWebhookSecretVersion(ctx context.Context, projectID string, version int) error {
	_, err := r.exec.ExecContext(ctx, `
		UPDATE project_webhook_secrets
		SET retired_at = COALESCE(retired_at, GREATEST(now(), created_at))
		WHERE project_id = $1 AND version = $2
	`, projectID, version)
	return err
}

const projectSelect = `
	SELECT id::text, name, api_key_hash, webhook_url, webhook_config_version,
		current_webhook_secret_version, ip_whitelist, created_at, updated_at
	FROM projects`

type rowScanner interface {
	Scan(...any) error
}

func scanProject(row rowScanner) (domain.Project, error) {
	var project domain.Project
	var webhookURL sql.NullString
	var secretVersion sql.NullInt64
	err := row.Scan(
		&project.ID,
		&project.Name,
		&project.APIKeyDigest,
		&webhookURL,
		&project.WebhookConfigVersion,
		&secretVersion,
		pq.Array(&project.IPWhitelist),
		&project.CreatedAt,
		&project.UpdatedAt,
	)
	if err != nil {
		return domain.Project{}, err
	}
	if webhookURL.Valid {
		project.WebhookURL = &webhookURL.String
	}
	if secretVersion.Valid {
		value := int(secretVersion.Int64)
		project.CurrentWebhookSecretVersion = &value
	}
	if project.IPWhitelist == nil {
		project.IPWhitelist = []string{}
	}
	return project, nil
}

type postgresDeviceTypeRepository struct {
	exec postgresExecutor
}

func (r *postgresDeviceTypeRepository) GetByCode(ctx context.Context, code string) (domain.DeviceType, error) {
	return scanDeviceType(r.exec.QueryRowContext(ctx, deviceTypeSelect+` WHERE code = $1`, code))
}

func (r *postgresDeviceTypeRepository) GetProfile(ctx context.Context, deviceTypeID string, revision int) (domain.DeviceTypeProfile, error) {
	var profile domain.DeviceTypeProfile
	var profileJSON []byte
	err := r.exec.QueryRowContext(ctx, `
		SELECT device_type_id::text, revision, profile, profile_hash, created_at
		FROM device_type_profiles
		WHERE device_type_id = $1 AND revision = $2
	`, deviceTypeID, revision).Scan(
		&profile.DeviceTypeID,
		&profile.Revision,
		&profileJSON,
		&profile.ProfileHash,
		&profile.CreatedAt,
	)
	if err != nil {
		return domain.DeviceTypeProfile{}, err
	}
	actions, err := decodeCapabilityActions(profileJSON)
	if err != nil {
		return domain.DeviceTypeProfile{}, err
	}
	profile.Actions = actions
	return profile, nil
}

func (r *postgresDeviceTypeRepository) List(ctx context.Context) ([]domain.DeviceType, error) {
	rows, err := r.exec.QueryContext(ctx, deviceTypeSelect+` ORDER BY code`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.DeviceType{}
	for rows.Next() {
		item, err := scanDeviceType(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const deviceTypeSelect = `
	SELECT id::text, code, current_revision, name, created_at, updated_at
	FROM device_types`

func scanDeviceType(row rowScanner) (domain.DeviceType, error) {
	var deviceType domain.DeviceType
	err := row.Scan(
		&deviceType.ID,
		&deviceType.Code,
		&deviceType.CurrentRevision,
		&deviceType.Name,
		&deviceType.CreatedAt,
		&deviceType.UpdatedAt,
	)
	return deviceType, err
}

type capabilityProfileJSON struct {
	Actions []struct {
		Identifier                    domain.ActionIdentifier `json:"identifier"`
		PayloadSchema                 map[string]any          `json:"payload_schema"`
		RiskLevel                     string                  `json:"risk_level"`
		DeliveryPolicy                domain.DeliveryPolicy   `json:"delivery_policy"`
		DispatchDeadlineMS            int64                   `json:"dispatch_deadline_ms"`
		ProviderRequestTimeoutMS      int64                   `json:"provider_request_timeout_ms"`
		ResultObservationTimeoutMS    int64                   `json:"result_observation_timeout_ms"`
		RetryAllowed                  bool                    `json:"retry_allowed"`
		DeliveryPolicyOverrideAllowed bool                    `json:"delivery_policy_override_allowed"`
	} `json:"actions"`
}

func decodeCapabilityActions(data []byte) ([]domain.CapabilityAction, error) {
	var raw capabilityProfileJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode Device Type profile: %w", err)
	}
	actions := make([]domain.CapabilityAction, 0, len(raw.Actions))
	for _, action := range raw.Actions {
		actions = append(actions, domain.CapabilityAction{
			Identifier:                    action.Identifier,
			PayloadSchema:                 action.PayloadSchema,
			RiskLevel:                     action.RiskLevel,
			DeliveryPolicy:                action.DeliveryPolicy,
			DispatchDeadline:              time.Duration(action.DispatchDeadlineMS) * time.Millisecond,
			ProviderRequestTimeout:        time.Duration(action.ProviderRequestTimeoutMS) * time.Millisecond,
			ResultObservationTimeout:      time.Duration(action.ResultObservationTimeoutMS) * time.Millisecond,
			RetryAllowed:                  action.RetryAllowed,
			DeliveryPolicyOverrideAllowed: action.DeliveryPolicyOverrideAllowed,
		})
	}
	return actions, nil
}

type postgresDeviceRepository struct {
	exec postgresExecutor
}

func (r *postgresDeviceRepository) Get(ctx context.Context, id string) (domain.Device, error) {
	return scanDevice(r.exec.QueryRowContext(ctx, deviceSelect+` WHERE d.id = $1`, id))
}

func (r *postgresDeviceRepository) GetByProject(ctx context.Context, projectID, id string) (domain.Device, error) {
	return scanDevice(r.exec.QueryRowContext(ctx, deviceSelect+` WHERE d.project_id = $1 AND d.id = $2`, projectID, id))
}

func (r *postgresDeviceRepository) GetByProviderIdentity(ctx context.Context, providerCode, providerDeviceID string) (domain.Device, error) {
	return scanDevice(r.exec.QueryRowContext(ctx, deviceSelect+`
		WHERE lower(d.provider_code) = lower($1) AND d.provider_device_id = $2
			AND d.lifecycle_status <> 'deleted'
	`, providerCode, providerDeviceID))
}

func (r *postgresDeviceRepository) ListByProject(ctx context.Context, projectID string) ([]domain.Device, error) {
	rows, err := r.exec.QueryContext(ctx, deviceSelect+` WHERE d.project_id = $1 ORDER BY d.created_at, d.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Device{}
	for rows.Next() {
		item, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *postgresDeviceRepository) List(ctx context.Context, request ListDevicesRequest) ([]domain.Device, int64, error) {
	if request.Limit < 1 || request.Limit > 100 || request.Offset < 0 {
		return nil, 0, ErrInvalidRepositoryRequest
	}
	const where = `
		WHERE ($1::uuid IS NULL OR d.project_id = $1)
			AND ($2::text IS NULL OR dt.code = $2)
			AND ($3::text IS NULL OR d.provider_code = $3)
			AND ($4::text IS NULL OR d.connection_status = $4)
			AND ($5::text IS NULL OR d.lifecycle_status = $5)`
	args := []any{
		nullableString(request.ProjectID),
		nullableString(request.DeviceTypeCode),
		nullableString(request.ProviderCode),
		nullableConnectionStatus(request.ConnectionStatus),
		nullableLifecycleStatus(request.LifecycleStatus),
	}
	var total int64
	if err := r.exec.QueryRowContext(ctx, `
		SELECT count(*) FROM devices d JOIN device_types dt ON dt.id = d.device_type_id
	`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.exec.QueryContext(ctx, deviceSelect+where+`
		ORDER BY d.created_at DESC, d.id DESC LIMIT $6 OFFSET $7
	`, append(args, request.Limit, request.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]domain.Device, 0, request.Limit)
	for rows.Next() {
		item, err := scanDevice(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *postgresDeviceRepository) GetCurrentState(ctx context.Context, deviceID string) (domain.DeviceState, error) {
	var state domain.DeviceState
	var stateJSON []byte
	var reportedAt sql.NullTime
	err := r.exec.QueryRowContext(ctx, `
		SELECT id::text, device_id::text, state, evidence_status, reported_at,
			observed_at, raw_message_id::text, created_at
		FROM device_states
		WHERE device_id = $1
		ORDER BY observed_at DESC, created_at DESC, id DESC
		LIMIT 1
	`, deviceID).Scan(
		&state.ID,
		&state.DeviceID,
		&stateJSON,
		&state.EvidenceStatus,
		&reportedAt,
		&state.ObservedAt,
		&state.RawMessageID,
		&state.CreatedAt,
	)
	if err != nil {
		return domain.DeviceState{}, err
	}
	if err := json.Unmarshal(stateJSON, &state.State); err != nil {
		return domain.DeviceState{}, fmt.Errorf("decode Device State: %w", err)
	}
	if reportedAt.Valid {
		state.ReportedAt = &reportedAt.Time
	}
	return state, nil
}

func (r *postgresDeviceRepository) Create(ctx context.Context, device domain.Device) error {
	_, err := r.exec.ExecContext(ctx, `
		INSERT INTO devices (
			id, project_id, device_type_id, name, provider_code, provider_device_id,
			access_type, transport_protocol, adapter, connection_status,
			lifecycle_status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`,
		device.ID,
		device.ProjectID,
		device.DeviceTypeID,
		device.Name,
		device.ProviderCode,
		device.ProviderDeviceID,
		device.AccessType,
		device.TransportProtocol,
		device.Adapter,
		device.ConnectionStatus,
		device.LifecycleStatus,
		device.CreatedAt,
		device.UpdatedAt,
	)
	return err
}

func (r *postgresDeviceRepository) GetForUpdate(ctx context.Context, id string) (domain.Device, error) {
	return scanDevice(r.exec.QueryRowContext(ctx, deviceSelect+` WHERE d.id = $1 FOR UPDATE OF d`, id))
}

func (r *postgresDeviceRepository) Rename(ctx context.Context, id, name string) error {
	_, err := r.exec.ExecContext(ctx, `UPDATE devices SET name = $2, updated_at = now() WHERE id = $1`, id, name)
	return err
}

func (r *postgresDeviceRepository) SetLifecycleStatus(ctx context.Context, id string, from, to domain.LifecycleStatus) (bool, error) {
	result, err := r.exec.ExecContext(ctx, `
		UPDATE devices SET lifecycle_status = $3, updated_at = now()
		WHERE id = $1 AND lifecycle_status = $2
	`, id, from, to)
	return exactlyOneRow(result, err)
}

func (r *postgresDeviceRepository) SetConnectionStatus(ctx context.Context, id string, from, to domain.ConnectionStatus) (bool, error) {
	result, err := r.exec.ExecContext(ctx, `
		UPDATE devices SET connection_status = $3, updated_at = now()
		WHERE id = $1 AND connection_status = $2
	`, id, from, to)
	return exactlyOneRow(result, err)
}

func (r *postgresDeviceRepository) SaveState(ctx context.Context, state domain.DeviceState) error {
	encodedState, err := json.Marshal(state.State)
	if err != nil {
		return fmt.Errorf("encode Device State: %w", err)
	}
	_, err = r.exec.ExecContext(ctx, `
		INSERT INTO device_states (
			id, device_id, state, evidence_status, reported_at, observed_at,
			raw_message_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		state.ID,
		state.DeviceID,
		encodedState,
		state.EvidenceStatus,
		nullableTime(state.ReportedAt),
		state.ObservedAt,
		state.RawMessageID,
		state.CreatedAt,
	)
	return err
}

const deviceSelect = `
	SELECT d.id::text, d.project_id::text, d.device_type_id::text, dt.code,
		d.name, d.provider_code, d.provider_device_id, d.access_type,
		d.transport_protocol, d.adapter, d.connection_status, d.lifecycle_status,
		d.created_at, d.updated_at
	FROM devices d
	JOIN device_types dt ON dt.id = d.device_type_id`

func scanDevice(row rowScanner) (domain.Device, error) {
	var device domain.Device
	err := row.Scan(
		&device.ID,
		&device.ProjectID,
		&device.DeviceTypeID,
		&device.DeviceTypeCode,
		&device.Name,
		&device.ProviderCode,
		&device.ProviderDeviceID,
		&device.AccessType,
		&device.TransportProtocol,
		&device.Adapter,
		&device.ConnectionStatus,
		&device.LifecycleStatus,
		&device.CreatedAt,
		&device.UpdatedAt,
	)
	return device, err
}

func exactlyOneRow(result sql.Result, err error) (bool, error) {
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func nullableString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableConnectionStatus(value *domain.ConnectionStatus) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableLifecycleStatus(value *domain.LifecycleStatus) any {
	if value == nil {
		return nil
	}
	return *value
}

var (
	_ ProjectStore      = (*PostgresStore)(nil)
	_ DeviceStore       = (*PostgresStore)(nil)
	_ ProjectRepository = (*postgresProjectRepository)(nil)
	_ DeviceTypeQueries = (*postgresDeviceTypeRepository)(nil)
	_ DeviceRepository  = (*postgresDeviceRepository)(nil)
)

package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

const webhookAttemptLimit = 5

type postgresWebhookRepository struct {
	exec postgresExecutor
}

func (r *postgresWebhookRepository) GetDelivery(ctx context.Context, id string) (domain.WebhookDelivery, error) {
	return scanWebhookDelivery(r.exec.QueryRowContext(ctx, webhookDeliverySelect+` WHERE id = $1`, id))
}

func (r *postgresWebhookRepository) ListAttempts(ctx context.Context, deliveryID string) ([]domain.WebhookDeliveryAttempt, error) {
	rows, err := r.exec.QueryContext(ctx, webhookAttemptSelect+` WHERE delivery_id = $1 ORDER BY attempt_no`, deliveryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.WebhookDeliveryAttempt{}
	for rows.Next() {
		item, err := scanWebhookAttempt(rows)
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

func (r *postgresWebhookRepository) CreateDelivery(ctx context.Context, request CreateWebhookDeliveryRequest) (domain.WebhookDelivery, bool, error) {
	if strings.TrimSpace(request.ID) == "" || strings.TrimSpace(request.EventID) == "" || len(request.RawBody) == 0 {
		return domain.WebhookDelivery{}, false, ErrInvalidRepositoryRequest
	}
	var projectID string
	err := r.exec.QueryRowContext(ctx, `SELECT project_id::text FROM device_events WHERE id = $1`, request.EventID).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WebhookDelivery{}, false, ErrWebhookEventNotFound
	}
	if err != nil {
		return domain.WebhookDelivery{}, false, err
	}
	config, configured, err := r.lockCurrentWebhookConfig(ctx, projectID)
	if err != nil {
		return domain.WebhookDelivery{}, false, err
	}
	if !configured {
		return domain.WebhookDelivery{}, false, nil
	}
	if err := r.insertDelivery(ctx, request.ID, projectID, request.EventID, request.RawBody, config, nil); err != nil {
		return domain.WebhookDelivery{}, false, err
	}
	delivery, err := r.GetDelivery(ctx, request.ID)
	return delivery, err == nil, err
}

func (r *postgresWebhookRepository) CreateReplay(ctx context.Context, request CreateWebhookReplayRequest) (domain.WebhookDelivery, error) {
	if strings.TrimSpace(request.ID) == "" || strings.TrimSpace(request.ReplayOfDeliveryID) == "" || request.ID == request.ReplayOfDeliveryID {
		return domain.WebhookDelivery{}, ErrInvalidRepositoryRequest
	}
	var projectID string
	err := r.exec.QueryRowContext(ctx, `SELECT project_id::text FROM webhook_deliveries WHERE id = $1`, request.ReplayOfDeliveryID).Scan(&projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WebhookDelivery{}, ErrWebhookDeliveryNotFound
	}
	if err != nil {
		return domain.WebhookDelivery{}, err
	}
	config, configured, err := r.lockCurrentWebhookConfig(ctx, projectID)
	if err != nil {
		return domain.WebhookDelivery{}, err
	}
	var eventID string
	var rawBody []byte
	var status domain.WebhookDeliveryStatus
	err = r.exec.QueryRowContext(ctx, `
		SELECT event_id::text, raw_body, status
		FROM webhook_deliveries
		WHERE id = $1 AND project_id = $2
		FOR UPDATE
	`, request.ReplayOfDeliveryID, projectID).Scan(&eventID, &rawBody, &status)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WebhookDelivery{}, ErrWebhookDeliveryNotFound
	}
	if err != nil {
		return domain.WebhookDelivery{}, err
	}
	if status != domain.WebhookDeliveryStatusDead {
		return domain.WebhookDelivery{}, ErrWebhookDeliveryNotDead
	}
	if !configured {
		return domain.WebhookDelivery{}, ErrWebhookNotConfigured
	}
	if err := r.insertDelivery(ctx, request.ID, projectID, eventID, rawBody, config, &request.ReplayOfDeliveryID); err != nil {
		return domain.WebhookDelivery{}, err
	}
	return r.GetDelivery(ctx, request.ID)
}

type currentWebhookConfig struct {
	targetURL     string
	configVersion int64
	secretVersion int
}

func (r *postgresWebhookRepository) lockCurrentWebhookConfig(ctx context.Context, projectID string) (currentWebhookConfig, bool, error) {
	var targetURL sql.NullString
	var secretVersion sql.NullInt64
	var configVersion int64
	err := r.exec.QueryRowContext(ctx, `
		SELECT /* lock_current_webhook_config */
			webhook_url, webhook_config_version, current_webhook_secret_version
		FROM projects
		WHERE id = $1
		FOR UPDATE
	`, projectID).Scan(&targetURL, &configVersion, &secretVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return currentWebhookConfig{}, false, nil
	}
	if err != nil {
		return currentWebhookConfig{}, false, err
	}
	if !targetURL.Valid || strings.TrimSpace(targetURL.String) == "" || configVersion <= 0 || !secretVersion.Valid || secretVersion.Int64 <= 0 {
		return currentWebhookConfig{}, false, nil
	}
	return currentWebhookConfig{
		targetURL:     targetURL.String,
		configVersion: configVersion,
		secretVersion: int(secretVersion.Int64),
	}, true, nil
}

func (r *postgresWebhookRepository) insertDelivery(ctx context.Context, id, projectID, eventID string, rawBody []byte, config currentWebhookConfig, replayOfDeliveryID *string) error {
	_, err := r.exec.ExecContext(ctx, `
		INSERT INTO webhook_deliveries (
			id, project_id, event_id, target_url, webhook_config_version,
			webhook_secret_version, raw_body, attempt_count, status,
			next_attempt_at, replay_of_delivery_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, 0, 'pending',
			now(), $8, now(), now()
		)
	`,
		id,
		projectID,
		eventID,
		config.targetURL,
		config.configVersion,
		config.secretVersion,
		rawBody,
		nullableString(replayOfDeliveryID),
	)
	return err
}

func (r *postgresWebhookRepository) ClaimDue(ctx context.Context, request ClaimWebhookRequest) (domain.WebhookDelivery, domain.WebhookDeliveryAttempt, bool, error) {
	if strings.TrimSpace(request.WorkerID) == "" || strings.TrimSpace(request.LeaseToken) == "" ||
		request.LeaseDuration < time.Microsecond || !validWebhookMaxAttempts(request.MaxAttempts) {
		return domain.WebhookDelivery{}, domain.WebhookDeliveryAttempt{}, false, nil
	}
	var deliveryID string
	err := r.exec.QueryRowContext(ctx, `
		SELECT id::text
		FROM webhook_deliveries
		WHERE status IN ('pending', 'failed')
			AND next_attempt_at <= now()
			AND attempt_count < $1
			AND lease_token IS NULL
		ORDER BY next_attempt_at, created_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, request.MaxAttempts).Scan(&deliveryID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WebhookDelivery{}, domain.WebhookDeliveryAttempt{}, false, nil
	}
	if err != nil {
		return domain.WebhookDelivery{}, domain.WebhookDeliveryAttempt{}, false, err
	}
	result, err := r.exec.ExecContext(ctx, `
		UPDATE webhook_deliveries
		SET status = 'sending', attempt_count = attempt_count + 1,
			next_attempt_at = NULL, lease_token = $2, lease_owner = $3,
			lease_expires_at = now() + $4 * interval '1 microsecond', updated_at = now()
		WHERE id = $1 AND status IN ('pending', 'failed')
			AND next_attempt_at <= now() AND attempt_count < $5 AND lease_token IS NULL
	`, deliveryID, request.LeaseToken, request.WorkerID, request.LeaseDuration.Microseconds(), request.MaxAttempts)
	updated, err := exactlyOneRow(result, err)
	if err != nil || !updated {
		return domain.WebhookDelivery{}, domain.WebhookDeliveryAttempt{}, false, err
	}
	attemptID, err := newUUID()
	if err != nil {
		return domain.WebhookDelivery{}, domain.WebhookDeliveryAttempt{}, false, err
	}
	_, err = r.exec.ExecContext(ctx, `
		INSERT INTO webhook_delivery_attempts (
			id, delivery_id, attempt_no, request_timestamp, started_at
		)
		SELECT $1, id, attempt_count, floor(extract(epoch FROM now()))::bigint, now()
		FROM webhook_deliveries WHERE id = $2
	`, attemptID, deliveryID)
	if err != nil {
		return domain.WebhookDelivery{}, domain.WebhookDeliveryAttempt{}, false, err
	}
	delivery, err := r.GetDelivery(ctx, deliveryID)
	if err != nil {
		return domain.WebhookDelivery{}, domain.WebhookDeliveryAttempt{}, false, err
	}
	attempt, err := r.getAttempt(ctx, attemptID)
	if err != nil {
		return domain.WebhookDelivery{}, domain.WebhookDeliveryAttempt{}, false, err
	}
	return delivery, attempt, true, nil
}

func (r *postgresWebhookRepository) ExhaustRetryBudget(ctx context.Context, maxAttempts int) (domain.WebhookDelivery, bool, error) {
	if !validWebhookMaxAttempts(maxAttempts) {
		return domain.WebhookDelivery{}, false, nil
	}
	var deliveryID string
	err := r.exec.QueryRowContext(ctx, `
		SELECT id::text
		FROM webhook_deliveries
		WHERE status = 'failed' AND attempt_count >= $1 AND lease_token IS NULL
		ORDER BY updated_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, maxAttempts).Scan(&deliveryID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WebhookDelivery{}, false, nil
	}
	if err != nil {
		return domain.WebhookDelivery{}, false, err
	}
	result, err := r.exec.ExecContext(ctx, `
		UPDATE webhook_deliveries
		SET status = 'dead', next_attempt_at = NULL, updated_at = now()
		WHERE id = $1 AND status = 'failed' AND attempt_count >= $2 AND lease_token IS NULL
	`, deliveryID, maxAttempts)
	updated, err := exactlyOneRow(result, err)
	if err != nil || !updated {
		return domain.WebhookDelivery{}, updated, err
	}
	delivery, err := r.GetDelivery(ctx, deliveryID)
	return delivery, err == nil, err
}

func (r *postgresWebhookRepository) CompleteAttempt(ctx context.Context, deliveryID, leaseToken string, request CompleteWebhookAttemptRequest) (bool, error) {
	locked, err := r.lockDeliveryAttempt(ctx, deliveryID, request.AttemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if locked.status != domain.WebhookDeliveryStatusSending || locked.leaseToken != leaseToken || !locked.leaseValid ||
		locked.attemptCompleted || locked.attemptNo != locked.attemptCount || !validWebhookMaxAttempts(request.MaxAttempts) {
		return false, nil
	}
	nextStatus, retry, valid := webhookCompletion(locked.attemptCount, request.MaxAttempts, request.HTTPStatus, request.ErrorCode, request.ErrorDetail, request.RetryDelay)
	if !valid {
		return false, nil
	}
	if _, err := r.exec.ExecContext(ctx, `
		UPDATE webhook_delivery_attempts
		SET http_status = $2, response_summary = $3, error_code = $4,
			error_detail = $5, completed_at = now()
		WHERE id = $1 AND delivery_id = $6 AND completed_at IS NULL
	`, request.AttemptID, nullableInt(request.HTTPStatus), nullableString(request.ResponseSummary), nullableString(request.ErrorCode), nullableString(request.ErrorDetail), deliveryID); err != nil {
		return false, err
	}
	return r.finishDelivery(ctx, deliveryID, nextStatus, request.ErrorCode, request.ErrorDetail, retry)
}

func (r *postgresWebhookRepository) RecoverNextExpiredSending(ctx context.Context, request RecoverExpiredWebhookRequest) (domain.WebhookDelivery, bool, error) {
	if strings.TrimSpace(request.ErrorCode) == "" || !validWebhookRetrySchedule(request.MaxAttempts, request.RetrySchedule) {
		return domain.WebhookDelivery{}, false, nil
	}
	var deliveryID string
	var attemptCount int
	err := r.exec.QueryRowContext(ctx, `
		SELECT id::text, attempt_count
		FROM webhook_deliveries
		WHERE status = 'sending' AND lease_expires_at <= now()
			AND EXISTS (
				SELECT 1 FROM webhook_delivery_attempts a
				WHERE a.delivery_id = webhook_deliveries.id
					AND a.attempt_no = webhook_deliveries.attempt_count
					AND a.completed_at IS NULL
			)
		ORDER BY lease_expires_at, id
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&deliveryID, &attemptCount)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WebhookDelivery{}, false, nil
	}
	if err != nil {
		return domain.WebhookDelivery{}, false, err
	}
	var attemptID string
	err = r.exec.QueryRowContext(ctx, `
		SELECT id::text
		FROM webhook_delivery_attempts
		WHERE delivery_id = $1 AND attempt_no = $2 AND completed_at IS NULL
		FOR UPDATE
	`, deliveryID, attemptCount).Scan(&attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.WebhookDelivery{}, false, nil
	}
	if err != nil {
		return domain.WebhookDelivery{}, false, err
	}
	nextStatus := domain.WebhookDeliveryStatusDead
	retry := time.Duration(0)
	if attemptCount < request.MaxAttempts {
		nextStatus = domain.WebhookDeliveryStatusFailed
		retry = request.RetrySchedule[attemptCount-1]
	}
	if _, err := r.exec.ExecContext(ctx, `
		UPDATE webhook_delivery_attempts
		SET error_code = $2, error_detail = $3, completed_at = now()
		WHERE id = $1 AND delivery_id = $4 AND completed_at IS NULL
	`, attemptID, request.ErrorCode, request.ErrorDetail, deliveryID); err != nil {
		return domain.WebhookDelivery{}, false, err
	}
	errorCode := request.ErrorCode
	errorDetail := request.ErrorDetail
	updated, err := r.finishDelivery(ctx, deliveryID, nextStatus, &errorCode, &errorDetail, retry)
	if err != nil || !updated {
		return domain.WebhookDelivery{}, updated, err
	}
	delivery, err := r.GetDelivery(ctx, deliveryID)
	return delivery, err == nil, err
}

func (r *postgresWebhookRepository) finishDelivery(ctx context.Context, deliveryID string, status domain.WebhookDeliveryStatus, errorCode, errorDetail *string, retryDelay time.Duration) (bool, error) {
	var result sql.Result
	var err error
	switch status {
	case domain.WebhookDeliveryStatusDelivered:
		result, err = r.exec.ExecContext(ctx, `
			UPDATE webhook_deliveries
			SET status = 'delivered', last_error_code = NULL, last_error_detail = NULL,
				next_attempt_at = NULL, lease_token = NULL, lease_owner = NULL,
				lease_expires_at = NULL, delivered_at = now(), updated_at = now()
			WHERE id = $1 AND status = 'sending'
		`, deliveryID)
	case domain.WebhookDeliveryStatusFailed:
		result, err = r.exec.ExecContext(ctx, `
			UPDATE webhook_deliveries
			SET status = 'failed', last_error_code = $2, last_error_detail = $3,
				next_attempt_at = now() + $4 * interval '1 microsecond',
				lease_token = NULL, lease_owner = NULL, lease_expires_at = NULL, updated_at = now()
			WHERE id = $1 AND status = 'sending'
		`, deliveryID, nullableString(errorCode), nullableString(errorDetail), retryDelay.Microseconds())
	case domain.WebhookDeliveryStatusDead:
		result, err = r.exec.ExecContext(ctx, `
			UPDATE webhook_deliveries
			SET status = 'dead', last_error_code = $2, last_error_detail = $3,
				next_attempt_at = NULL, lease_token = NULL, lease_owner = NULL,
				lease_expires_at = NULL, updated_at = now()
			WHERE id = $1 AND status = 'sending'
		`, deliveryID, nullableString(errorCode), nullableString(errorDetail))
	default:
		return false, nil
	}
	return exactlyOneRow(result, err)
}

type lockedWebhookDeliveryAttempt struct {
	status           domain.WebhookDeliveryStatus
	attemptCount     int
	leaseToken       string
	leaseValid       bool
	attemptNo        int
	attemptCompleted bool
}

func (r *postgresWebhookRepository) lockDeliveryAttempt(ctx context.Context, deliveryID, attemptID string) (lockedWebhookDeliveryAttempt, error) {
	var locked lockedWebhookDeliveryAttempt
	var leaseToken sql.NullString
	var leaseValid sql.NullBool
	err := r.exec.QueryRowContext(ctx, `
		SELECT status, attempt_count, lease_token::text, lease_expires_at > now()
		FROM webhook_deliveries
		WHERE id = $1
		FOR UPDATE
	`, deliveryID).Scan(&locked.status, &locked.attemptCount, &leaseToken, &leaseValid)
	if err != nil {
		return locked, err
	}
	if leaseToken.Valid {
		locked.leaseToken = leaseToken.String
	}
	locked.leaseValid = leaseValid.Valid && leaseValid.Bool
	err = r.exec.QueryRowContext(ctx, `
		SELECT attempt_no, completed_at IS NOT NULL
		FROM webhook_delivery_attempts
		WHERE id = $2 AND delivery_id = $1
		FOR UPDATE
	`, deliveryID, attemptID).Scan(&locked.attemptNo, &locked.attemptCompleted)
	return locked, err
}

func (r *postgresWebhookRepository) getAttempt(ctx context.Context, attemptID string) (domain.WebhookDeliveryAttempt, error) {
	return scanWebhookAttempt(r.exec.QueryRowContext(ctx, webhookAttemptSelect+` WHERE id = $1`, attemptID))
}

func validWebhookMaxAttempts(value int) bool {
	return value >= 1 && value <= webhookAttemptLimit
}

func validWebhookRetrySchedule(maxAttempts int, schedule []time.Duration) bool {
	if !validWebhookMaxAttempts(maxAttempts) || len(schedule) != maxAttempts-1 {
		return false
	}
	for _, delay := range schedule {
		if delay < time.Microsecond {
			return false
		}
	}
	return true
}

func webhookCompletion(attemptCount, maxAttempts int, httpStatus *int, errorCode, errorDetail *string, retryDelay time.Duration) (domain.WebhookDeliveryStatus, time.Duration, bool) {
	if httpStatus != nil && (*httpStatus < 100 || *httpStatus > 599) {
		return "", 0, false
	}
	if httpStatus != nil && *httpStatus >= 200 && *httpStatus <= 299 {
		if errorCode != nil || errorDetail != nil || retryDelay != 0 {
			return "", 0, false
		}
		return domain.WebhookDeliveryStatusDelivered, 0, true
	}
	if errorCode == nil || strings.TrimSpace(*errorCode) == "" {
		return "", 0, false
	}
	if attemptCount >= maxAttempts {
		if retryDelay != 0 {
			return "", 0, false
		}
		return domain.WebhookDeliveryStatusDead, 0, true
	}
	if retryDelay < time.Microsecond {
		return "", 0, false
	}
	return domain.WebhookDeliveryStatusFailed, retryDelay, true
}

const webhookDeliverySelect = `
	SELECT id::text, project_id::text, event_id::text, target_url,
		webhook_config_version, webhook_secret_version, raw_body,
		attempt_count, status, last_error_code, last_error_detail,
		next_attempt_at, lease_token::text, lease_owner, lease_expires_at,
		replay_of_delivery_id::text, delivered_at, created_at, updated_at
	FROM webhook_deliveries`

func scanWebhookDelivery(row rowScanner) (domain.WebhookDelivery, error) {
	var delivery domain.WebhookDelivery
	var lastErrorCode, lastErrorDetail, leaseToken, leaseOwner, replayID sql.NullString
	var nextAttemptAt, leaseExpiresAt, deliveredAt sql.NullTime
	err := row.Scan(
		&delivery.ID, &delivery.ProjectID, &delivery.EventID, &delivery.TargetURL,
		&delivery.WebhookConfigVersion, &delivery.WebhookSecretVersion, &delivery.RawBody,
		&delivery.AttemptCount, &delivery.Status, &lastErrorCode, &lastErrorDetail,
		&nextAttemptAt, &leaseToken, &leaseOwner, &leaseExpiresAt, &replayID,
		&deliveredAt, &delivery.CreatedAt, &delivery.UpdatedAt,
	)
	if err != nil {
		return domain.WebhookDelivery{}, err
	}
	delivery.LastErrorCode = stringPointer(lastErrorCode)
	delivery.LastErrorDetail = stringPointer(lastErrorDetail)
	delivery.NextAttemptAt = timePointer(nextAttemptAt)
	delivery.LeaseToken = stringPointer(leaseToken)
	delivery.LeaseOwner = stringPointer(leaseOwner)
	delivery.LeaseExpiresAt = timePointer(leaseExpiresAt)
	delivery.ReplayOfDeliveryID = stringPointer(replayID)
	delivery.DeliveredAt = timePointer(deliveredAt)
	return delivery, nil
}

const webhookAttemptSelect = `
	SELECT id::text, delivery_id::text, attempt_no, request_timestamp,
		http_status, response_summary, error_code, error_detail, started_at, completed_at
	FROM webhook_delivery_attempts`

func scanWebhookAttempt(row rowScanner) (domain.WebhookDeliveryAttempt, error) {
	var attempt domain.WebhookDeliveryAttempt
	var httpStatus sql.NullInt64
	var responseSummary, errorCode, errorDetail sql.NullString
	var completedAt sql.NullTime
	err := row.Scan(
		&attempt.ID, &attempt.DeliveryID, &attempt.AttemptNo, &attempt.RequestTimestamp,
		&httpStatus, &responseSummary, &errorCode, &errorDetail, &attempt.StartedAt, &completedAt,
	)
	if err != nil {
		return domain.WebhookDeliveryAttempt{}, err
	}
	attempt.HTTPStatus = intPointer(httpStatus)
	attempt.ResponseSummary = stringPointer(responseSummary)
	attempt.ErrorCode = stringPointer(errorCode)
	attempt.ErrorDetail = stringPointer(errorDetail)
	attempt.CompletedAt = timePointer(completedAt)
	return attempt, nil
}

type postgresSimulatorRepository struct {
	exec postgresExecutor
}

func (r *postgresSimulatorRepository) Get(ctx context.Context) (domain.SimulatorConfig, error) {
	var config domain.SimulatorConfig
	var delayMilliseconds int64
	err := r.exec.QueryRowContext(ctx, `
		SELECT outcome, delay_ms, version, updated_at
		FROM simulator_config WHERE singleton
	`).Scan(&config.Outcome, &delayMilliseconds, &config.Version, &config.UpdatedAt)
	if err != nil {
		return domain.SimulatorConfig{}, err
	}
	config.Delay = time.Duration(delayMilliseconds) * time.Millisecond
	return config, nil
}

func (r *postgresSimulatorRepository) Update(ctx context.Context, expectedVersion int64, request UpdateSimulatorRequest) (bool, error) {
	if expectedVersion <= 0 || !validSimulatorUpdate(request) {
		return false, nil
	}
	result, err := r.exec.ExecContext(ctx, `
		UPDATE simulator_config
		SET outcome = $2, delay_ms = $3, version = version + 1, updated_at = now()
		WHERE singleton AND version = $1
	`, expectedVersion, request.Outcome, request.Delay.Milliseconds())
	return exactlyOneRow(result, err)
}

func validSimulatorUpdate(request UpdateSimulatorRequest) bool {
	if request.Delay < 0 || request.Delay > 60*time.Second || request.Delay%time.Millisecond != 0 {
		return false
	}
	switch request.Outcome {
	case domain.SimulatorOutcomeProviderAccepted,
		domain.SimulatorOutcomeProviderRejected,
		domain.SimulatorOutcomeTransportErrorBeforeSend,
		domain.SimulatorOutcomeTransportErrorAfterSend,
		domain.SimulatorOutcomeInvalidResponse:
		return true
	default:
		return false
	}
}

func intPointer(value sql.NullInt64) *int {
	if !value.Valid {
		return nil
	}
	result := int(value.Int64)
	return &result
}

package repository

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

type postgresCommandRepository struct {
	exec postgresExecutor
}

func (r *postgresCommandRepository) Get(ctx context.Context, id string) (domain.Command, error) {
	return scanCommand(r.exec.QueryRowContext(ctx, commandSelect+` WHERE id = $1`, id))
}

func (r *postgresCommandRepository) GetByIdempotencyKey(ctx context.Context, projectID, idempotencyKey string) (domain.Command, error) {
	return scanCommand(r.exec.QueryRowContext(ctx, commandSelect+` WHERE project_id = $1 AND idempotency_key = $2`, projectID, idempotencyKey))
}

func (r *postgresCommandRepository) List(ctx context.Context, request ListCommandsRequest) ([]domain.Command, int64, error) {
	if request.Limit < 1 || request.Limit > 100 || request.Offset < 0 {
		return nil, 0, ErrInvalidRepositoryRequest
	}
	rows, err := r.exec.QueryContext(ctx, commandSelect+`
		WHERE ($1::uuid IS NULL OR project_id = $1)
			AND ($2::uuid IS NULL OR device_id = $2)
			AND ($3::text IS NULL OR command_type = $3)
			AND ($4::text IS NULL OR status = $4)
		ORDER BY created_at DESC, id DESC
		LIMIT $5 OFFSET $6
	`, nullableString(request.ProjectID), nullableString(request.DeviceID), nullableActionIdentifier(request.CommandType),
		nullableCommandStatus(request.Status), request.Limit, request.Offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]domain.Command, 0)
	for rows.Next() {
		item, scanErr := scanCommand(rows)
		if scanErr != nil {
			return nil, 0, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	var total int64
	err = r.exec.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM device_commands
		WHERE ($1::uuid IS NULL OR project_id = $1)
			AND ($2::uuid IS NULL OR device_id = $2)
			AND ($3::text IS NULL OR command_type = $3)
			AND ($4::text IS NULL OR status = $4)
	`, nullableString(request.ProjectID), nullableString(request.DeviceID), nullableActionIdentifier(request.CommandType),
		nullableCommandStatus(request.Status)).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *postgresCommandRepository) ListAttempts(ctx context.Context, commandID string) ([]domain.CommandAttempt, error) {
	rows, err := r.exec.QueryContext(ctx, commandAttemptSelect+` WHERE command_id = $1 ORDER BY attempt_no`, commandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.CommandAttempt{}
	for rows.Next() {
		item, err := scanCommandAttempt(rows)
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

func (r *postgresCommandRepository) Create(ctx context.Context, command domain.Command) error {
	payload, err := marshalObject(command.Payload)
	if err != nil {
		return fmt.Errorf("encode Command payload: %w", err)
	}
	_, err = r.exec.ExecContext(ctx, `
		INSERT INTO device_commands (
			id, project_id, device_id, device_type_id, command_type, payload,
			device_type_revision, delivery_policy, status, reason_code, reason_detail,
			confirmation_level, evidence_status, idempotency_key, request_hash,
			queued_at, dispatch_deadline_at, sent_at, result_deadline_at,
			finished_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
			$12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22
		)
	`,
		command.ID,
		command.ProjectID,
		command.DeviceID,
		command.DeviceTypeID,
		command.CommandType,
		payload,
		command.DeviceTypeRevision,
		command.DeliveryPolicy,
		command.Status,
		nullableString(command.ReasonCode),
		nullableString(command.ReasonDetail),
		command.ConfirmationLevel,
		command.EvidenceStatus,
		command.IdempotencyKey,
		command.RequestHash,
		command.QueuedAt,
		command.DispatchDeadlineAt,
		nullableTime(command.SentAt),
		nullableTime(command.ResultDeadlineAt),
		nullableTime(command.FinishedAt),
		command.CreatedAt,
		command.UpdatedAt,
	)
	return err
}

func (r *postgresCommandRepository) GetForUpdate(ctx context.Context, id string) (domain.Command, error) {
	return scanCommand(r.exec.QueryRowContext(ctx, commandSelect+` WHERE id = $1 FOR UPDATE`, id))
}

func (r *postgresCommandRepository) ClaimNext(ctx context.Context, request ClaimCommandRequest) (domain.Command, domain.CommandAttempt, bool, error) {
	if request.LeaseDuration < time.Microsecond {
		return domain.Command{}, domain.CommandAttempt{}, false, nil
	}
	command, err := scanCommand(r.exec.QueryRowContext(ctx, commandSelect+`
		WHERE id = (
			SELECT c.id
			FROM device_commands c
			JOIN devices d ON d.id = c.device_id
				WHERE c.status = 'queued'
					AND c.dispatch_deadline_at > now()
					AND d.provider_code = $1
					AND d.adapter = $2
				AND NOT EXISTS (
					SELECT 1 FROM device_command_attempts a
					WHERE a.command_id = c.id AND a.phase <> 'completed'
				)
			ORDER BY c.queued_at, c.id
			FOR UPDATE OF c SKIP LOCKED
			LIMIT 1
		)
		`, request.ProviderCode, request.Adapter))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Command{}, domain.CommandAttempt{}, false, nil
	}
	if err != nil {
		return domain.Command{}, domain.CommandAttempt{}, false, err
	}
	var attemptNo int
	if err := r.exec.QueryRowContext(ctx, `
		SELECT COALESCE(max(attempt_no), 0) + 1
		FROM device_command_attempts WHERE command_id = $1
	`, command.ID).Scan(&attemptNo); err != nil {
		return domain.Command{}, domain.CommandAttempt{}, false, err
	}
	attemptID, err := newUUID()
	if err != nil {
		return domain.Command{}, domain.CommandAttempt{}, false, err
	}
	requestSummary, err := marshalObject(request.RequestSummary)
	if err != nil {
		return domain.Command{}, domain.CommandAttempt{}, false, fmt.Errorf("encode Attempt request summary: %w", err)
	}
	_, err = r.exec.ExecContext(ctx, `
		INSERT INTO device_command_attempts (
			id, command_id, attempt_no, phase, provider_code, adapter,
			provider_request_key, request_summary, response_summary,
			confirmation_level, evidence_status, lease_token, lease_owner,
			lease_expires_at, claimed_at
		) VALUES (
			$1, $2, $3, 'claimed', $4, $5, $6, $7, '{}'::jsonb,
			'none', 'none', $8, $9,
			LEAST(now() + $10 * interval '1 microsecond', $11), now()
		)
	`,
		attemptID,
		command.ID,
		attemptNo,
		request.ProviderCode,
		request.Adapter,
		request.RequestKey,
		requestSummary,
		request.LeaseToken,
		request.WorkerID,
		request.LeaseDuration.Microseconds(),
		command.DispatchDeadlineAt,
	)
	if err != nil {
		return domain.Command{}, domain.CommandAttempt{}, false, err
	}
	attempt, err := r.getAttempt(ctx, attemptID)
	if err != nil {
		return domain.Command{}, domain.CommandAttempt{}, false, err
	}
	return command, attempt, true, nil
}

func (r *postgresCommandRepository) ReclaimAttempt(ctx context.Context, attemptID, expiredToken string, request ReclaimAttemptRequest) (domain.CommandAttempt, bool, error) {
	if request.LeaseDuration < time.Microsecond || request.LeaseToken == expiredToken {
		return domain.CommandAttempt{}, false, nil
	}
	var commandID string
	err := r.exec.QueryRowContext(ctx, `
		SELECT command_id::text FROM device_command_attempts WHERE id = $1
	`, attemptID).Scan(&commandID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CommandAttempt{}, false, nil
	}
	if err != nil {
		return domain.CommandAttempt{}, false, err
	}
	locked, err := r.lockCommandAttempt(ctx, commandID, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CommandAttempt{}, false, nil
	}
	if err != nil {
		return domain.CommandAttempt{}, false, err
	}
	if locked.commandStatus != domain.CommandStatusQueued || !locked.dispatchDeadlineValid || locked.attemptPhase != domain.AttemptPhaseClaimed || locked.leaseToken != expiredToken || locked.leaseValid {
		return domain.CommandAttempt{}, false, nil
	}
	result, err := r.exec.ExecContext(ctx, `
		UPDATE device_command_attempts
		SET lease_owner = $3, lease_token = $4,
			lease_expires_at = LEAST(
				now() + $5 * interval '1 microsecond',
				(SELECT dispatch_deadline_at FROM device_commands WHERE id = $6)
			)
		WHERE id = $1 AND lease_token = $2 AND phase = 'claimed'
			AND lease_expires_at <= now()
	`, attemptID, expiredToken, request.WorkerID, request.LeaseToken, request.LeaseDuration.Microseconds(), commandID)
	updated, err := exactlyOneRow(result, err)
	if err != nil || !updated {
		return domain.CommandAttempt{}, updated, err
	}
	attempt, err := r.getAttempt(ctx, attemptID)
	return attempt, err == nil, err
}

func (r *postgresCommandRepository) MarkDispatching(ctx context.Context, commandID, attemptID, leaseToken string, resultObservationTimeout time.Duration) (bool, error) {
	if resultObservationTimeout < time.Microsecond {
		return false, nil
	}
	locked, err := r.lockCommandAttempt(ctx, commandID, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if locked.commandStatus != domain.CommandStatusQueued || !locked.dispatchDeadlineValid || locked.attemptPhase != domain.AttemptPhaseClaimed || locked.leaseToken != leaseToken || !locked.leaseValid {
		return false, nil
	}
	if _, err := r.exec.ExecContext(ctx, `
		UPDATE device_command_attempts SET phase = 'dispatching', dispatching_at = now()
		WHERE id = $1
	`, attemptID); err != nil {
		return false, err
	}
	result, err := r.exec.ExecContext(ctx, `
		UPDATE device_commands
		SET status = 'sent', sent_at = now(),
			result_deadline_at = now() + $2 * interval '1 microsecond',
			updated_at = now()
		WHERE id = $1
	`, commandID, resultObservationTimeout.Microseconds())
	return exactlyOneRow(result, err)
}

func (r *postgresCommandRepository) CompleteAttempt(ctx context.Context, commandID, attemptID, expectedLeaseToken string, request CompleteCommandAttemptRequest) (bool, error) {
	locked, err := r.lockCommandAttempt(ctx, commandID, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if locked.attemptPhase == domain.AttemptPhaseCompleted || locked.leaseToken != expectedLeaseToken || !locked.leaseValid {
		return false, nil
	}
	if (request.Outcome == domain.AttemptOutcomeInvalidRequest && locked.commandStatus != domain.CommandStatusQueued) ||
		(request.Outcome != domain.AttemptOutcomeInvalidRequest && locked.commandStatus != domain.CommandStatusSent) ||
		!attemptCompletionAllowed(locked.providerCode, locked.attemptPhase, request) {
		return false, nil
	}
	responseSummary, err := marshalObject(request.ResponseSummary)
	if err != nil {
		return false, fmt.Errorf("encode Attempt response summary: %w", err)
	}
	result, err := r.exec.ExecContext(ctx, `
		UPDATE device_command_attempts
		SET phase = 'completed', outcome = $2, confirmation_level = $3,
			evidence_status = $4, response_summary = $5, error_code = $6,
			error_detail = $7, completed_at = now()
		WHERE id = $1
	`,
		attemptID,
		request.Outcome,
		request.ConfirmationLevel,
		request.EvidenceStatus,
		responseSummary,
		nullableString(request.ErrorCode),
		nullableString(request.ErrorDetail),
	)
	return exactlyOneRow(result, err)
}

func (r *postgresCommandRepository) RecoverExpiredDispatching(ctx context.Context, commandID, attemptID, expiredLeaseToken string) (bool, error) {
	locked, err := r.lockCommandAttempt(ctx, commandID, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if locked.commandStatus != domain.CommandStatusSent || locked.attemptPhase != domain.AttemptPhaseDispatching || locked.leaseToken != expiredLeaseToken || locked.leaseValid {
		return false, nil
	}
	if _, err := r.exec.ExecContext(ctx, `
		UPDATE device_command_attempts
		SET phase = 'completed', outcome = 'transport_error_after_send',
			confirmation_level = 'transport_sent', evidence_status = 'unverified',
			response_summary = '{}'::jsonb, error_code = 'worker_lease_expired',
			error_detail = NULL, completed_at = now()
		WHERE id = $1 AND command_id = $2 AND phase = 'dispatching' AND lease_token = $3
	`, attemptID, commandID, expiredLeaseToken); err != nil {
		return false, err
	}
	result, err := r.exec.ExecContext(ctx, `
		UPDATE device_commands
		SET status = 'unknown', reason_code = 'provider_delivery_unknown',
			reason_detail = NULL, confirmation_level = 'transport_sent',
			evidence_status = 'unverified', finished_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'sent'
	`, commandID)
	return exactlyOneRow(result, err)
}

func (r *postgresCommandRepository) CancelQueued(ctx context.Context, commandID string, reasonDetail *string) (bool, error) {
	return r.finishQueued(ctx, commandID, domain.CommandStatusCancelled, "cancelled_by_request", reasonDetail, false)
}

func (r *postgresCommandRepository) ExpireQueued(ctx context.Context, commandID string) (bool, error) {
	return r.finishQueued(ctx, commandID, domain.CommandStatusTimeout, "dispatch_deadline_exceeded", nil, true)
}

func (r *postgresCommandRepository) ExpireResultObservation(ctx context.Context, commandID string) (bool, error) {
	var status domain.CommandStatus
	err := r.exec.QueryRowContext(ctx, `
		SELECT status FROM device_commands
		WHERE id = $1 AND result_deadline_at <= now()
		FOR UPDATE
	`, commandID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if status != domain.CommandStatusSent && status != domain.CommandStatusAcked {
		return false, nil
	}
	var incompleteAttemptID string
	err = r.exec.QueryRowContext(ctx, `
		SELECT id::text
		FROM device_command_attempts
		WHERE command_id = $1 AND phase <> 'completed'
		FOR UPDATE
	`, commandID).Scan(&incompleteAttemptID)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	result, err := r.exec.ExecContext(ctx, `
		UPDATE device_commands
		SET status = 'timeout', reason_code = 'result_observation_timeout',
			reason_detail = NULL, finished_at = now(), updated_at = now()
		WHERE id = $1 AND status = $2 AND result_deadline_at <= now()
	`, commandID, status)
	return exactlyOneRow(result, err)
}

func (r *postgresCommandRepository) UpdateEvidenceFromAttempt(ctx context.Context, commandID, attemptID, expectedLeaseToken string, expectedStatus domain.CommandStatus) (bool, error) {
	locked, err := r.lockCommandAttempt(ctx, commandID, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if locked.commandStatus != expectedStatus || expectedStatus != domain.CommandStatusSent || locked.attemptPhase != domain.AttemptPhaseCompleted ||
		locked.leaseToken != expectedLeaseToken || locked.attemptOutcome == nil || *locked.attemptOutcome != domain.AttemptOutcomeProviderAccepted ||
		locked.attemptConfirmation != domain.ConfirmationProviderAccepted ||
		!evidenceProgresses(locked.commandConfirmation, locked.commandEvidence, locked.attemptConfirmation, locked.attemptEvidence) {
		return false, nil
	}
	result, err := r.exec.ExecContext(ctx, `
		UPDATE device_commands
		SET confirmation_level = $2, evidence_status = $3, updated_at = now()
		WHERE id = $1 AND status = $4
	`, commandID, locked.attemptConfirmation, locked.attemptEvidence, expectedStatus)
	return exactlyOneRow(result, err)
}

func (r *postgresCommandRepository) TransitionFromAttempt(ctx context.Context, commandID, attemptID, expectedLeaseToken string, transition CommandStatusTransition) (bool, error) {
	locked, err := r.lockCommandAttempt(ctx, commandID, attemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if locked.commandStatus != transition.From || locked.attemptPhase != domain.AttemptPhaseCompleted || locked.leaseToken != expectedLeaseToken ||
		locked.attemptOutcome == nil || locked.attemptConfirmation != transition.ConfirmationLevel || locked.attemptEvidence != transition.EvidenceStatus ||
		!domain.CanTransitionCommand(transition.From, transition.To) || !attemptTransitionMatches(*locked.attemptOutcome, transition) ||
		!evidenceDoesNotRegress(locked.commandConfirmation, locked.commandEvidence, transition.ConfirmationLevel, transition.EvidenceStatus) {
		return false, nil
	}
	return r.updateCommandTransition(ctx, commandID, transition)
}

func (r *postgresCommandRepository) UpdateProviderAcceptanceFromVerifiedMessage(ctx context.Context, commandID string, request VerifiedEvidenceUpdateRequest) (bool, error) {
	locked, err := r.lockCommandAttempt(ctx, commandID, request.AttemptID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if locked.commandStatus != request.ExpectedStatus || request.ExpectedStatus != domain.CommandStatusSent ||
		locked.attemptPhase != domain.AttemptPhaseCompleted || locked.attemptOutcome == nil ||
		*locked.attemptOutcome != domain.AttemptOutcomeProviderAccepted || request.AttemptOutcome != domain.AttemptOutcomeProviderAccepted ||
		locked.providerCode != domain.ProviderCodeSimulator || locked.attemptConfirmation != domain.ConfirmationProviderAccepted ||
		!evidenceProgresses(locked.commandConfirmation, locked.commandEvidence, domain.ConfirmationProviderAccepted, domain.EvidenceVerified) ||
		!evidenceProgresses(locked.attemptConfirmation, locked.attemptEvidence, domain.ConfirmationProviderAccepted, domain.EvidenceVerified) {
		return false, nil
	}
	var evidenceMatches bool
	err = r.exec.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
				FROM device_raw_messages rm
				JOIN device_commands c ON c.id = $1
				JOIN device_command_attempts a ON a.id = $4 AND a.command_id = c.id
				JOIN devices d ON d.id = c.device_id
				WHERE rm.id = $2
					AND rm.deduplication_key = $3
					AND rm.deduplication_key = a.provider_request_key
					AND rm.device_id = c.device_id
					AND rm.provider_code = 'simulator'
					AND rm.provider_code = a.provider_code
					AND rm.adapter = a.adapter
					AND rm.provider_code = d.provider_code
					AND rm.provider_device_id = d.provider_device_id
					AND rm.direction = 'inbound'
			)
		`, commandID, request.RawMessageID, request.RawMessageDeduplicationKey, request.AttemptID).Scan(&evidenceMatches)
	if err != nil {
		return false, err
	}
	if !evidenceMatches {
		return false, nil
	}
	responseSummary, err := marshalObject(request.ResponseSummary)
	if err != nil {
		return false, fmt.Errorf("encode verified response summary: %w", err)
	}
	result, err := r.exec.ExecContext(ctx, `
		UPDATE device_command_attempts
		SET outcome = $2, confirmation_level = 'provider_accepted', evidence_status = 'verified',
			response_summary = $3
		WHERE id = $1 AND command_id = $4 AND phase = 'completed'
	`,
		request.AttemptID,
		request.AttemptOutcome,
		responseSummary,
		commandID,
	)
	updated, err := exactlyOneRow(result, err)
	if err != nil || !updated {
		return false, err
	}
	result, err = r.exec.ExecContext(ctx, `
		UPDATE device_commands
		SET confirmation_level = 'provider_accepted', evidence_status = 'verified', updated_at = now()
		WHERE id = $1 AND status = $2
	`, commandID, request.ExpectedStatus)
	return exactlyOneRow(result, err)
}

func (r *postgresCommandRepository) finishQueued(ctx context.Context, commandID string, status domain.CommandStatus, reasonCode string, reasonDetail *string, requireExpired bool) (bool, error) {
	var commandStatus domain.CommandStatus
	var dispatchDeadlineExpired bool
	err := r.exec.QueryRowContext(ctx, `
		SELECT status, dispatch_deadline_at <= now()
		FROM device_commands WHERE id = $1 FOR UPDATE
	`, commandID).Scan(&commandStatus, &dispatchDeadlineExpired)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if commandStatus != domain.CommandStatusQueued || (requireExpired && !dispatchDeadlineExpired) {
		return false, nil
	}
	var attemptID, phase, leaseToken string
	var leaseValid bool
	err = r.exec.QueryRowContext(ctx, `
		SELECT id::text, phase, lease_token::text, lease_expires_at > now()
		FROM device_command_attempts
		WHERE command_id = $1 AND phase <> 'completed'
		FOR UPDATE
	`, commandID).Scan(&attemptID, &phase, &leaseToken, &leaseValid)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil {
		if leaseValid || domain.AttemptPhase(phase) != domain.AttemptPhaseClaimed {
			return false, nil
		}
		if _, err := r.exec.ExecContext(ctx, `
			UPDATE device_command_attempts
			SET phase = 'completed', outcome = 'not_dispatched',
				confirmation_level = 'none', evidence_status = 'none',
				response_summary = '{}'::jsonb, error_code = $2,
				error_detail = $3, completed_at = now()
			WHERE id = $1 AND lease_token = $4
		`, attemptID, reasonCode, nullableString(reasonDetail), leaseToken); err != nil {
			return false, err
		}
	}
	result, err := r.exec.ExecContext(ctx, `
		UPDATE device_commands
		SET status = $2, reason_code = $3, reason_detail = $4,
			confirmation_level = 'none', evidence_status = 'none',
			finished_at = now(), updated_at = now()
			WHERE id = $1
	`, commandID, status, reasonCode, nullableString(reasonDetail))
	return exactlyOneRow(result, err)
}

func (r *postgresCommandRepository) updateCommandTransition(ctx context.Context, commandID string, transition CommandStatusTransition) (bool, error) {
	result, err := r.exec.ExecContext(ctx, `
		UPDATE device_commands
		SET status = $2, reason_code = $3, reason_detail = $4,
			confirmation_level = $5, evidence_status = $6,
			finished_at = now(), updated_at = now()
		WHERE id = $1 AND status = $7
	`,
		commandID,
		transition.To,
		nullableString(transition.ReasonCode),
		nullableString(transition.ReasonDetail),
		transition.ConfirmationLevel,
		transition.EvidenceStatus,
		transition.From,
	)
	return exactlyOneRow(result, err)
}

type lockedCommandAttempt struct {
	commandStatus         domain.CommandStatus
	commandConfirmation   domain.ConfirmationLevel
	commandEvidence       domain.EvidenceStatus
	dispatchDeadlineValid bool
	attemptPhase          domain.AttemptPhase
	attemptOutcome        *domain.AttemptOutcome
	attemptConfirmation   domain.ConfirmationLevel
	attemptEvidence       domain.EvidenceStatus
	providerCode          string
	leaseToken            string
	leaseValid            bool
}

func (r *postgresCommandRepository) lockCommandAttempt(ctx context.Context, commandID, attemptID string) (lockedCommandAttempt, error) {
	var locked lockedCommandAttempt
	var attemptOutcome sql.NullString
	err := r.exec.QueryRowContext(ctx, `
		SELECT status, confirmation_level, evidence_status,
			dispatch_deadline_at > now()
		FROM device_commands
		WHERE id = $1
		FOR UPDATE
	`, commandID).Scan(
		&locked.commandStatus,
		&locked.commandConfirmation,
		&locked.commandEvidence,
		&locked.dispatchDeadlineValid,
	)
	if err != nil {
		return locked, err
	}
	err = r.exec.QueryRowContext(ctx, `
		SELECT phase, outcome, confirmation_level, evidence_status,
			provider_code, lease_token::text, lease_expires_at > now()
		FROM device_command_attempts
		WHERE id = $2 AND command_id = $1
		FOR UPDATE
	`, commandID, attemptID).Scan(
		&locked.attemptPhase,
		&attemptOutcome,
		&locked.attemptConfirmation,
		&locked.attemptEvidence,
		&locked.providerCode,
		&locked.leaseToken,
		&locked.leaseValid,
	)
	if attemptOutcome.Valid {
		value := domain.AttemptOutcome(attemptOutcome.String)
		locked.attemptOutcome = &value
	}
	return locked, err
}

func evidenceProgresses(currentLevel domain.ConfirmationLevel, currentEvidence domain.EvidenceStatus, nextLevel domain.ConfirmationLevel, nextEvidence domain.EvidenceStatus) bool {
	if !domain.CanAdvanceConfirmation(currentLevel, nextLevel) {
		return false
	}
	if nextLevel != currentLevel {
		return true
	}
	return currentEvidence != nextEvidence && currentEvidence != domain.EvidenceVerified && nextEvidence == domain.EvidenceVerified
}

func evidenceDoesNotRegress(currentLevel domain.ConfirmationLevel, currentEvidence domain.EvidenceStatus, nextLevel domain.ConfirmationLevel, nextEvidence domain.EvidenceStatus) bool {
	if !domain.CanAdvanceConfirmation(currentLevel, nextLevel) {
		return false
	}
	return nextLevel != currentLevel || currentEvidence != domain.EvidenceVerified || nextEvidence == domain.EvidenceVerified
}

func attemptCompletionAllowed(providerCode string, phase domain.AttemptPhase, request CompleteCommandAttemptRequest) bool {
	if request.Outcome == domain.AttemptOutcomeInvalidRequest {
		return providerCode == domain.ProviderCodeWWTIOT && phase == domain.AttemptPhaseClaimed &&
			request.ConfirmationLevel == domain.ConfirmationNone && request.EvidenceStatus == domain.EvidenceNone
	}
	if phase != domain.AttemptPhaseDispatching {
		return false
	}
	switch providerCode {
	case domain.ProviderCodeWWTIOT:
		switch request.Outcome {
		case domain.AttemptOutcomeTransportErrorBeforeSend:
			return request.ConfirmationLevel == domain.ConfirmationNone && request.EvidenceStatus == domain.EvidenceNone
		case domain.AttemptOutcomeTransportErrorAfterSend, domain.AttemptOutcomeInvalidResponse:
			return request.ConfirmationLevel == domain.ConfirmationTransportSent && request.EvidenceStatus == domain.EvidenceVerified
		case domain.AttemptOutcomeProviderRejected:
			return request.ConfirmationLevel == domain.ConfirmationTransportSent && request.EvidenceStatus == domain.EvidenceUnverified
		case domain.AttemptOutcomeProviderAccepted:
			return request.ConfirmationLevel == domain.ConfirmationProviderAccepted && request.EvidenceStatus == domain.EvidenceUnverified
		default:
			return false
		}
	case domain.ProviderCodeSimulator:
		switch request.Outcome {
		case domain.AttemptOutcomeTransportErrorBeforeSend:
			return request.ConfirmationLevel == domain.ConfirmationNone && request.EvidenceStatus == domain.EvidenceNone
		case domain.AttemptOutcomeTransportErrorAfterSend, domain.AttemptOutcomeInvalidResponse, domain.AttemptOutcomeProviderRejected:
			return request.ConfirmationLevel == domain.ConfirmationTransportSent && request.EvidenceStatus == domain.EvidenceVerified
		case domain.AttemptOutcomeProviderAccepted:
			return request.ConfirmationLevel == domain.ConfirmationProviderAccepted && request.EvidenceStatus == domain.EvidenceVerified
		default:
			return false
		}
	default:
		return false
	}
}

func attemptTransitionMatches(outcome domain.AttemptOutcome, transition CommandStatusTransition) bool {
	reason := ""
	if transition.ReasonCode != nil {
		reason = *transition.ReasonCode
	}
	switch outcome {
	case domain.AttemptOutcomeInvalidRequest:
		return transition.From == domain.CommandStatusQueued && transition.To == domain.CommandStatusFailed && reason == "provider_not_configured"
	case domain.AttemptOutcomeTransportErrorBeforeSend:
		return transition.From == domain.CommandStatusSent && transition.To == domain.CommandStatusFailed && reason == "provider_transport_error"
	case domain.AttemptOutcomeProviderRejected:
		return transition.From == domain.CommandStatusSent && transition.To == domain.CommandStatusFailed && reason == "provider_rejected"
	case domain.AttemptOutcomeTransportErrorAfterSend:
		return transition.From == domain.CommandStatusSent && transition.To == domain.CommandStatusUnknown && reason == "provider_delivery_unknown"
	case domain.AttemptOutcomeInvalidResponse:
		return transition.From == domain.CommandStatusSent && transition.To == domain.CommandStatusUnknown && reason == "provider_response_invalid"
	default:
		return false
	}
}

func (r *postgresCommandRepository) getAttempt(ctx context.Context, id string) (domain.CommandAttempt, error) {
	return scanCommandAttempt(r.exec.QueryRowContext(ctx, commandAttemptSelect+` WHERE id = $1`, id))
}

const commandSelect = `
	SELECT id::text, project_id::text, device_id::text, device_type_id::text,
		command_type, payload, device_type_revision, delivery_policy, status,
		reason_code, reason_detail, confirmation_level, evidence_status,
		idempotency_key, request_hash, queued_at, dispatch_deadline_at,
		sent_at, result_deadline_at, finished_at, created_at, updated_at
	FROM device_commands`

func scanCommand(row rowScanner) (domain.Command, error) {
	var command domain.Command
	var payload []byte
	var reasonCode, reasonDetail sql.NullString
	var sentAt, resultDeadlineAt, finishedAt sql.NullTime
	err := row.Scan(
		&command.ID,
		&command.ProjectID,
		&command.DeviceID,
		&command.DeviceTypeID,
		&command.CommandType,
		&payload,
		&command.DeviceTypeRevision,
		&command.DeliveryPolicy,
		&command.Status,
		&reasonCode,
		&reasonDetail,
		&command.ConfirmationLevel,
		&command.EvidenceStatus,
		&command.IdempotencyKey,
		&command.RequestHash,
		&command.QueuedAt,
		&command.DispatchDeadlineAt,
		&sentAt,
		&resultDeadlineAt,
		&finishedAt,
		&command.CreatedAt,
		&command.UpdatedAt,
	)
	if err != nil {
		return domain.Command{}, err
	}
	if err := json.Unmarshal(payload, &command.Payload); err != nil {
		return domain.Command{}, fmt.Errorf("decode Command payload: %w", err)
	}
	command.ReasonCode = stringPointer(reasonCode)
	command.ReasonDetail = stringPointer(reasonDetail)
	command.SentAt = timePointer(sentAt)
	command.ResultDeadlineAt = timePointer(resultDeadlineAt)
	command.FinishedAt = timePointer(finishedAt)
	return command, nil
}

const commandAttemptSelect = `
	SELECT id::text, command_id::text, attempt_no, phase, provider_code,
		adapter, provider_request_key, outcome, confirmation_level,
		evidence_status, request_summary, response_summary, error_code,
		error_detail, lease_token::text, lease_owner, lease_expires_at,
		claimed_at, dispatching_at, completed_at
	FROM device_command_attempts`

func scanCommandAttempt(row rowScanner) (domain.CommandAttempt, error) {
	var attempt domain.CommandAttempt
	var outcome, errorCode, errorDetail sql.NullString
	var requestSummary, responseSummary []byte
	var dispatchingAt, completedAt sql.NullTime
	err := row.Scan(
		&attempt.ID,
		&attempt.CommandID,
		&attempt.AttemptNo,
		&attempt.Phase,
		&attempt.ProviderCode,
		&attempt.Adapter,
		&attempt.ProviderRequestKey,
		&outcome,
		&attempt.ConfirmationLevel,
		&attempt.EvidenceStatus,
		&requestSummary,
		&responseSummary,
		&errorCode,
		&errorDetail,
		&attempt.LeaseToken,
		&attempt.LeaseOwner,
		&attempt.LeaseExpiresAt,
		&attempt.ClaimedAt,
		&dispatchingAt,
		&completedAt,
	)
	if err != nil {
		return domain.CommandAttempt{}, err
	}
	if err := json.Unmarshal(requestSummary, &attempt.RequestSummary); err != nil {
		return domain.CommandAttempt{}, fmt.Errorf("decode Attempt request summary: %w", err)
	}
	if err := json.Unmarshal(responseSummary, &attempt.ResponseSummary); err != nil {
		return domain.CommandAttempt{}, fmt.Errorf("decode Attempt response summary: %w", err)
	}
	if outcome.Valid {
		value := domain.AttemptOutcome(outcome.String)
		attempt.Outcome = &value
	}
	attempt.ErrorCode = stringPointer(errorCode)
	attempt.ErrorDetail = stringPointer(errorDetail)
	attempt.DispatchingAt = timePointer(dispatchingAt)
	attempt.CompletedAt = timePointer(completedAt)
	return attempt, nil
}

type postgresRawMessageRepository struct {
	exec postgresExecutor
}

func (r *postgresRawMessageRepository) GetByDeduplicationKey(ctx context.Context, providerCode, deduplicationKey string) (domain.RawMessage, error) {
	return scanRawMessage(r.exec.QueryRowContext(ctx, rawMessageSelect+` WHERE provider_code = $1 AND deduplication_key = $2`, providerCode, deduplicationKey))
}

func (r *postgresRawMessageRepository) Create(ctx context.Context, message domain.RawMessage) error {
	headers, err := marshalObject(message.Headers)
	if err != nil {
		return fmt.Errorf("encode RawMessage headers: %w", err)
	}
	_, err = r.exec.ExecContext(ctx, `
		INSERT INTO device_raw_messages (
			id, device_id, provider_code, provider_device_id, access_type,
			transport_protocol, adapter, direction, deduplication_key,
			headers, body, received_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`,
		message.ID,
		nullableString(message.DeviceID),
		message.ProviderCode,
		message.ProviderDeviceID,
		message.AccessType,
		message.TransportProtocol,
		message.Adapter,
		message.Direction,
		message.DeduplicationKey,
		headers,
		message.Body,
		message.ReceivedAt,
		message.CreatedAt,
	)
	return err
}

const rawMessageSelect = `
	SELECT id::text, device_id::text, provider_code, provider_device_id,
		access_type, transport_protocol, adapter, direction, deduplication_key,
		headers, body, received_at, created_at
	FROM device_raw_messages`

func scanRawMessage(row rowScanner) (domain.RawMessage, error) {
	var message domain.RawMessage
	var deviceID sql.NullString
	var headers []byte
	err := row.Scan(
		&message.ID,
		&deviceID,
		&message.ProviderCode,
		&message.ProviderDeviceID,
		&message.AccessType,
		&message.TransportProtocol,
		&message.Adapter,
		&message.Direction,
		&message.DeduplicationKey,
		&headers,
		&message.Body,
		&message.ReceivedAt,
		&message.CreatedAt,
	)
	if err != nil {
		return domain.RawMessage{}, err
	}
	message.DeviceID = stringPointer(deviceID)
	if err := json.Unmarshal(headers, &message.Headers); err != nil {
		return domain.RawMessage{}, fmt.Errorf("decode RawMessage headers: %w", err)
	}
	return message, nil
}

type postgresEventRepository struct {
	exec postgresExecutor
}

func (r *postgresEventRepository) Get(ctx context.Context, id string) (domain.Event, error) {
	return scanEvent(r.exec.QueryRowContext(ctx, eventSelect+` WHERE id = $1`, id))
}

func (r *postgresEventRepository) GetByDeduplicationKey(ctx context.Context, projectID, deduplicationKey string) (domain.Event, error) {
	return scanEvent(r.exec.QueryRowContext(ctx, eventSelect+` WHERE project_id = $1 AND deduplication_key = $2`, projectID, deduplicationKey))
}

func (r *postgresEventRepository) ListByCommand(ctx context.Context, commandID string) ([]domain.Event, error) {
	rows, err := r.exec.QueryContext(ctx, eventSelect+` WHERE command_id = $1 ORDER BY occurred_at, id`, commandID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []domain.Event{}
	for rows.Next() {
		item, err := scanEvent(rows)
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

func nullableActionIdentifier(value *domain.ActionIdentifier) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func nullableCommandStatus(value *domain.CommandStatus) any {
	if value == nil {
		return nil
	}
	return string(*value)
}

func (r *postgresEventRepository) Create(ctx context.Context, event domain.Event) error {
	payload, err := marshalObject(event.Payload)
	if err != nil {
		return fmt.Errorf("encode Event payload: %w", err)
	}
	_, err = r.exec.ExecContext(ctx, `
		INSERT INTO device_events (
			id, schema_version, event_type, project_id, device_id, command_id,
			source, payload, raw_message_id, deduplication_key, occurred_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		event.ID,
		event.SchemaVersion,
		event.EventType,
		event.ProjectID,
		nullableString(event.DeviceID),
		nullableString(event.CommandID),
		event.Source,
		payload,
		nullableString(event.RawMessageID),
		event.DeduplicationKey,
		event.OccurredAt,
		event.CreatedAt,
	)
	return err
}

const eventSelect = `
	SELECT id::text, schema_version, event_type, project_id::text,
		device_id::text, command_id::text, source, payload,
		raw_message_id::text, deduplication_key, occurred_at, created_at
	FROM device_events`

func scanEvent(row rowScanner) (domain.Event, error) {
	var event domain.Event
	var deviceID, commandID, rawMessageID sql.NullString
	var payload []byte
	err := row.Scan(
		&event.ID,
		&event.SchemaVersion,
		&event.EventType,
		&event.ProjectID,
		&deviceID,
		&commandID,
		&event.Source,
		&payload,
		&rawMessageID,
		&event.DeduplicationKey,
		&event.OccurredAt,
		&event.CreatedAt,
	)
	if err != nil {
		return domain.Event{}, err
	}
	event.DeviceID = stringPointer(deviceID)
	event.CommandID = stringPointer(commandID)
	event.RawMessageID = stringPointer(rawMessageID)
	if err := json.Unmarshal(payload, &event.Payload); err != nil {
		return domain.Event{}, fmt.Errorf("decode Event payload: %w", err)
	}
	return event, nil
}

type postgresAuditRepository struct {
	exec postgresExecutor
}

func (r *postgresAuditRepository) Get(ctx context.Context, id string) (domain.AuditLog, error) {
	return scanAudit(r.exec.QueryRowContext(ctx, auditSelect+` WHERE id = $1`, id))
}

func (r *postgresAuditRepository) Create(ctx context.Context, log domain.AuditLog) error {
	metadata, err := marshalObject(log.Metadata)
	if err != nil {
		return fmt.Errorf("encode Audit metadata: %w", err)
	}
	_, err = r.exec.ExecContext(ctx, `
		INSERT INTO audit_logs (
			id, actor_type, actor_id, project_id, action, result,
			resource_type, resource_id, ip_address, request_id, metadata, occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		log.ID,
		log.ActorType,
		nullableString(log.ActorID),
		nullableString(log.ProjectID),
		log.Action,
		log.Result,
		log.ResourceType,
		nullableString(log.ResourceID),
		nullableString(log.IPAddress),
		nullableString(log.RequestID),
		metadata,
		log.OccurredAt,
	)
	return err
}

const auditSelect = `
	SELECT id::text, actor_type, actor_id, project_id::text, action, result,
		resource_type, resource_id, host(ip_address), request_id, metadata, occurred_at
	FROM audit_logs`

func scanAudit(row rowScanner) (domain.AuditLog, error) {
	var log domain.AuditLog
	var actorID, projectID, resourceID, ipAddress, requestID sql.NullString
	var metadata []byte
	err := row.Scan(
		&log.ID,
		&log.ActorType,
		&actorID,
		&projectID,
		&log.Action,
		&log.Result,
		&log.ResourceType,
		&resourceID,
		&ipAddress,
		&requestID,
		&metadata,
		&log.OccurredAt,
	)
	if err != nil {
		return domain.AuditLog{}, err
	}
	log.ActorID = stringPointer(actorID)
	log.ProjectID = stringPointer(projectID)
	log.ResourceID = stringPointer(resourceID)
	log.IPAddress = stringPointer(ipAddress)
	log.RequestID = stringPointer(requestID)
	if err := json.Unmarshal(metadata, &log.Metadata); err != nil {
		return domain.AuditLog{}, fmt.Errorf("decode Audit metadata: %w", err)
	}
	return log, nil
}

func marshalObject(value map[string]any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	return json.Marshal(value)
}

func newUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	), nil
}

func stringPointer(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func timePointer(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}

var (
	_ CommandRepository    = (*postgresCommandRepository)(nil)
	_ RawMessageRepository = (*postgresRawMessageRepository)(nil)
	_ EventRepository      = (*postgresEventRepository)(nil)
	_ AuditRepository      = (*postgresAuditRepository)(nil)
)

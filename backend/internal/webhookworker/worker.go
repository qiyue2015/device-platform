package webhookworker

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/projectservice"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const responseCaptureLimit = 4096

const maximumAttempts = 5

var defaultRetrySchedule = []time.Duration{time.Second, 5 * time.Second, 30 * time.Second, 2 * time.Minute}

type Worker struct {
	store         Store
	secrets       SecretResolver
	client        HTTPClient
	workerID      string
	pollInterval  time.Duration
	leaseDuration time.Duration
	maxAttempts   int
	retrySchedule []time.Duration
	random        io.Reader
	randomMu      sync.Mutex
}

func New(store Store, secrets SecretResolver, config Config) (*Worker, error) {
	if store == nil || secrets == nil || config.Client == nil {
		return nil, ErrInvalidConfig
	}
	if strings.TrimSpace(config.WorkerID) == "" {
		config.WorkerID = "webhook-worker"
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 500 * time.Millisecond
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 15 * time.Second
	}
	if config.MaxAttempts == 0 {
		config.MaxAttempts = maximumAttempts
	}
	if config.RetrySchedule == nil {
		if config.MaxAttempts >= 1 && config.MaxAttempts <= maximumAttempts {
			config.RetrySchedule = append([]time.Duration(nil), defaultRetrySchedule[:config.MaxAttempts-1]...)
		}
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	if config.MaxAttempts < 1 || config.MaxAttempts > maximumAttempts || len(config.RetrySchedule) != config.MaxAttempts-1 {
		return nil, ErrInvalidConfig
	}
	for index, delay := range config.RetrySchedule {
		if delay < defaultRetrySchedule[index] {
			return nil, ErrInvalidConfig
		}
	}
	return &Worker{
		store: store, secrets: secrets, client: config.Client, workerID: strings.TrimSpace(config.WorkerID),
		pollInterval: config.PollInterval, leaseDuration: config.LeaseDuration,
		maxAttempts: config.MaxAttempts, retrySchedule: append([]time.Duration(nil), config.RetrySchedule...), random: config.Random,
	}, nil
}

func (w *Worker) DispatchNext(ctx context.Context) (bool, error) {
	leaseToken, err := w.newUUID()
	if err != nil {
		return false, err
	}
	var delivery domain.WebhookDelivery
	var attempt domain.WebhookDeliveryAttempt
	var claimed bool
	err = w.store.TransactWebhookAudit(ctx, func(tx repository.WebhookAuditTx) error {
		var claimErr error
		delivery, attempt, claimed, claimErr = tx.Webhooks().ClaimDue(ctx, repository.ClaimWebhookRequest{
			WorkerID: w.workerID, LeaseToken: leaseToken, LeaseDuration: w.leaseDuration, MaxAttempts: w.maxAttempts,
		})
		return claimErr
	})
	if err != nil || !claimed {
		return false, err
	}
	return true, w.dispatchClaimed(ctx, delivery, attempt, leaseToken)
}

func (w *Worker) RecoverNext(ctx context.Context) (bool, error) {
	var recovered bool
	err := w.store.TransactWebhookAudit(ctx, func(tx repository.WebhookAuditTx) error {
		_, updated, err := tx.Webhooks().RecoverNextExpiredSending(ctx, repository.RecoverExpiredWebhookRequest{
			ErrorCode: "worker_lease_expired", ErrorDetail: "Webhook worker did not persist a result",
			RetrySchedule: w.retrySchedule, MaxAttempts: w.maxAttempts,
		})
		recovered = updated
		return err
	})
	return recovered, err
}

func (w *Worker) ExhaustNext(ctx context.Context) (bool, error) {
	var exhausted bool
	err := w.store.TransactWebhookAudit(ctx, func(tx repository.WebhookAuditTx) error {
		_, updated, err := tx.Webhooks().ExhaustRetryBudget(ctx, w.maxAttempts)
		exhausted = updated
		return err
	})
	return exhausted, err
}

func (w *Worker) Run(ctx context.Context, report ErrorReporter) {
	if report == nil {
		report = func(error) {}
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		for {
			worked, err := w.runNext(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					report(err)
				}
				break
			}
			if !worked || ctx.Err() != nil {
				break
			}
		}
		timer.Reset(w.pollInterval)
	}
}

func (w *Worker) runNext(ctx context.Context) (bool, error) {
	if recovered, err := w.RecoverNext(ctx); err != nil || recovered {
		return recovered, err
	}
	if exhausted, err := w.ExhaustNext(ctx); err != nil || exhausted {
		return exhausted, err
	}
	return w.DispatchNext(ctx)
}

func (w *Worker) dispatchClaimed(ctx context.Context, delivery domain.WebhookDelivery, attempt domain.WebhookDeliveryAttempt, leaseToken string) error {
	secret, err := w.secrets.ResolveWebhookSecret(ctx, delivery.ProjectID, delivery.WebhookSecretVersion)
	if err != nil {
		return w.completeSecretFailure(ctx, delivery, attempt, leaseToken, err)
	}
	request, err := webhookRequest(ctx, delivery, attempt, secret)
	if err != nil {
		return w.completeFailure(ctx, delivery, attempt, leaseToken, nil, nil, "invalid_target", "Webhook target URL is invalid")
	}
	response, err := w.client.Do(request)
	if err != nil {
		return w.completeFailure(ctx, delivery, attempt, leaseToken, nil, nil, "transport_error", "Webhook HTTP request failed")
	}
	defer response.Body.Close()
	summary := summarizeResponse(response)
	if response.StatusCode >= 200 && response.StatusCode <= 299 {
		return w.completeSuccess(ctx, delivery, attempt, leaseToken, response.StatusCode, summary)
	}
	return w.completeFailure(ctx, delivery, attempt, leaseToken, &response.StatusCode, &summary, "http_error", fmt.Sprintf("Webhook endpoint returned HTTP %d", response.StatusCode))
}

func webhookRequest(ctx context.Context, delivery domain.WebhookDelivery, attempt domain.WebhookDeliveryAttempt, secret string) (*http.Request, error) {
	if err := ValidateTargetURL(delivery.TargetURL); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.TargetURL, bytes.NewReader(delivery.RawBody))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Device-Platform-Timestamp", fmt.Sprintf("%d", attempt.RequestTimestamp))
	request.Header.Set("X-Device-Platform-Signature", signature(secret, attempt.RequestTimestamp, delivery.WebhookSecretVersion, delivery.RawBody))
	request.Header.Set("X-Device-Platform-Event-ID", delivery.EventID)
	request.Header.Set("X-Device-Platform-Secret-Version", strconv.Itoa(delivery.WebhookSecretVersion))
	return request, nil
}

func (w *Worker) completeSecretFailure(ctx context.Context, delivery domain.WebhookDelivery, attempt domain.WebhookDeliveryAttempt, leaseToken string, resolveErr error) error {
	errorCode := "secret_resolution_failed"
	errorDetail := "Webhook secret could not be resolved"
	decryptionFailure := errors.Is(resolveErr, projectservice.ErrWebhookSecretDecryption)
	var encryptionKeyVersion int
	if decryptionFailure {
		secretVersion, err := w.store.Projects().GetWebhookSecretVersion(ctx, delivery.ProjectID, delivery.WebhookSecretVersion)
		if err != nil {
			return fmt.Errorf("load failed Webhook secret metadata: %w", err)
		}
		encryptionKeyVersion = secretVersion.EncryptionKeyVersion
	}
	return w.complete(ctx, delivery, attempt, leaseToken, repository.CompleteWebhookAttemptRequest{
		AttemptID: attempt.ID, ErrorCode: &errorCode, ErrorDetail: &errorDetail,
		RetryDelay: w.retryDelay(delivery.AttemptCount), MaxAttempts: w.maxAttempts,
	}, func(tx repository.WebhookAuditTx) error {
		if !decryptionFailure {
			return nil
		}
		auditID, err := w.newUUID()
		if err != nil {
			return err
		}
		projectID := delivery.ProjectID
		return tx.Audits().Create(ctx, domain.AuditLog{
			ID: auditID, ActorType: domain.ActorTypeSystem, ProjectID: &projectID,
			Action: "project.webhook_secret_decryption_failed", Result: domain.AuditResultFailure,
			ResourceType: "project", ResourceID: &projectID,
			Metadata: map[string]any{
				"webhook_secret_version": delivery.WebhookSecretVersion,
				"encryption_key_version": encryptionKeyVersion,
				"error_code":             "secret_decryption_failed",
			}, OccurredAt: time.Now().UTC(),
		})
	})
}

func (w *Worker) completeSuccess(ctx context.Context, delivery domain.WebhookDelivery, attempt domain.WebhookDeliveryAttempt, leaseToken string, status int, summary string) error {
	return w.complete(ctx, delivery, attempt, leaseToken, repository.CompleteWebhookAttemptRequest{
		AttemptID: attempt.ID, HTTPStatus: &status, ResponseSummary: &summary, MaxAttempts: w.maxAttempts,
	}, nil)
}

func (w *Worker) completeFailure(ctx context.Context, delivery domain.WebhookDelivery, attempt domain.WebhookDeliveryAttempt, leaseToken string, status *int, summary *string, code, detail string) error {
	detail = truncate(detail, responseCaptureLimit)
	return w.complete(ctx, delivery, attempt, leaseToken, repository.CompleteWebhookAttemptRequest{
		AttemptID: attempt.ID, HTTPStatus: status, ResponseSummary: summary,
		ErrorCode: &code, ErrorDetail: &detail, RetryDelay: w.retryDelay(delivery.AttemptCount), MaxAttempts: w.maxAttempts,
	}, nil)
}

func (w *Worker) complete(ctx context.Context, delivery domain.WebhookDelivery, _ domain.WebhookDeliveryAttempt, leaseToken string, request repository.CompleteWebhookAttemptRequest, extra func(repository.WebhookAuditTx) error) error {
	return w.store.TransactWebhookAudit(ctx, func(tx repository.WebhookAuditTx) error {
		completed, err := tx.Webhooks().CompleteAttempt(ctx, delivery.ID, leaseToken, request)
		if err != nil {
			return err
		}
		if !completed {
			return ErrLeaseLost
		}
		if extra != nil {
			return extra(tx)
		}
		return nil
	})
}

func (w *Worker) retryDelay(attemptCount int) time.Duration {
	if attemptCount >= w.maxAttempts {
		return 0
	}
	return w.retrySchedule[attemptCount-1]
}

func (w *Worker) newUUID() (string, error) {
	w.randomMu.Lock()
	defer w.randomMu.Unlock()
	var value [16]byte
	if _, err := io.ReadFull(w.random, value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func signature(secret string, timestamp int64, secretVersion int, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = fmt.Fprintf(mac, "v1.%d.%d.", timestamp, secretVersion)
	_, _ = mac.Write(body)
	return "v1=" + hex.EncodeToString(mac.Sum(nil))
}

func summarizeResponse(response *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(response.Body, responseCaptureLimit+1))
	truncated := len(body) > responseCaptureLimit
	if truncated {
		body = body[:responseCaptureLimit]
	}
	digest := sha256.Sum256(body)
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	summary := struct {
		MediaType     string `json:"media_type,omitempty"`
		CapturedBytes int    `json:"captured_bytes"`
		Truncated     bool   `json:"truncated"`
		ReadError     bool   `json:"read_error,omitempty"`
		SHA256        string `json:"sha256"`
	}{
		MediaType: mediaType, CapturedBytes: len(body), Truncated: truncated,
		ReadError: err != nil, SHA256: hex.EncodeToString(digest[:]),
	}
	encoded, _ := json.Marshal(summary)
	return string(encoded)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

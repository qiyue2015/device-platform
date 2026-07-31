package simulator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const updateAttempts = 8

var (
	ErrInvalidRequest  = errors.New("invalid Simulator request")
	ErrConcurrentWrite = errors.New("concurrent Simulator update")
	errVersionChanged  = errors.New("Simulator version changed")
)

type Service struct {
	store  repository.SimulatorStore
	random io.Reader
	mu     sync.Mutex
}

type UpdateRequest struct {
	Outcome domain.SimulatorOutcome
	DelayMS int
}

type RequestMetadata struct {
	ActorType domain.ActorType
	ActorID   string
	IPAddress string
	RequestID string
}

func NewService(store repository.SimulatorStore, random io.Reader) *Service {
	if random == nil {
		random = rand.Reader
	}
	return &Service{store: store, random: random}
}

func (s *Service) Get(ctx context.Context) (domain.SimulatorConfig, error) {
	if s == nil || s.store == nil {
		return domain.SimulatorConfig{}, ErrInvalidRequest
	}
	return s.store.Simulator().Get(ctx)
}

func (s *Service) Update(ctx context.Context, request UpdateRequest, metadata RequestMetadata) (domain.SimulatorConfig, error) {
	if s == nil || s.store == nil || !validUpdate(request) || !validMetadata(metadata) {
		return domain.SimulatorConfig{}, ErrInvalidRequest
	}
	for range updateAttempts {
		var updatedConfig domain.SimulatorConfig
		err := s.store.TransactSimulator(ctx, func(tx repository.SimulatorTx) error {
			current, err := tx.Simulator().GetForUpdate(ctx)
			if err != nil {
				return err
			}
			updated, err := tx.Simulator().Update(ctx, current.Version, repository.UpdateSimulatorRequest{
				Outcome: request.Outcome, Delay: time.Duration(request.DelayMS) * time.Millisecond,
			})
			if err != nil {
				return err
			}
			if !updated {
				return errVersionChanged
			}
			updatedConfig, err = tx.Simulator().Get(ctx)
			if err != nil {
				return err
			}
			return s.createAudit(ctx, tx, updatedConfig, metadata)
		})
		if errors.Is(err, errVersionChanged) {
			continue
		}
		return updatedConfig, err
	}
	return domain.SimulatorConfig{}, ErrConcurrentWrite
}

func (s *Service) createAudit(ctx context.Context, tx repository.SimulatorTx, config domain.SimulatorConfig, metadata RequestMetadata) error {
	id, err := s.randomUUID()
	if err != nil {
		return err
	}
	return tx.Audits().Create(ctx, domain.AuditLog{
		ID: id, ActorType: metadata.ActorType, ActorID: optional(metadata.ActorID),
		Action: "simulator.updated", Result: domain.AuditResultSuccess,
		ResourceType: "simulator", IPAddress: optional(metadata.IPAddress), RequestID: optional(metadata.RequestID),
		Metadata: map[string]any{
			"outcome": config.Outcome, "delay_ms": config.Delay.Milliseconds(), "version": config.Version,
		},
		OccurredAt: config.UpdatedAt.UTC(),
	})
}

func (s *Service) randomUUID() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var value [16]byte
	if _, err := io.ReadFull(s.random, value[:]); err != nil {
		return "", fmt.Errorf("generate Simulator Audit ID: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func validUpdate(request UpdateRequest) bool {
	return validOutcome(request.Outcome) && request.DelayMS >= 0 && request.DelayMS <= 60000
}

func validMetadata(metadata RequestMetadata) bool {
	if metadata.ActorType != domain.ActorTypeAdmin || strings.TrimSpace(metadata.ActorID) == "" || strings.TrimSpace(metadata.RequestID) == "" {
		return false
	}
	return strings.TrimSpace(metadata.IPAddress) == "" || net.ParseIP(strings.TrimSpace(metadata.IPAddress)) != nil
}

func optional(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	copy := value
	return &copy
}

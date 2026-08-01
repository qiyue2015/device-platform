package omni

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
)

func TestRegistryRequiresOneExactProfileSession(t *testing.T) {
	registry := NewRegistry()
	bike, err := registry.Register(domain.ProviderProfileOmniBikeV207, testIMEI, testDeviceID, testProjectID, "bike-one", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if bike.Generation() != 1 {
		t.Fatalf("bike generation = %d", bike.Generation())
	}
	if _, err := registry.LookupUnique(domain.ProviderProfileOmniIoTV135, testIMEI); !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("cross-profile lookup error = %v", err)
	}
	duplicate, err := registry.Register(domain.ProviderProfileOmniBikeV207, testIMEI, testDeviceID, testProjectID, "bike-two", &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.Generation() != 2 {
		t.Fatalf("duplicate generation = %d", duplicate.Generation())
	}
	if _, err := registry.LookupUnique(domain.ProviderProfileOmniBikeV207, testIMEI); !errors.Is(err, ErrSessionAmbiguous) {
		t.Fatalf("duplicate lookup error = %v", err)
	}
	registry.Unregister(bike)
	got, err := registry.LookupUnique(domain.ProviderProfileOmniBikeV207, testIMEI)
	if err != nil || got != duplicate {
		t.Fatalf("lookup after old generation closes = %p, %v", got, err)
	}
	registry.Unregister(bike)
	got, err = registry.LookupUnique(domain.ProviderProfileOmniBikeV207, testIMEI)
	if err != nil || got != duplicate {
		t.Fatalf("stale unregister removed current session: %p, %v", got, err)
	}
}

func TestRegistryWritesWholeFrameAcrossShortWrites(t *testing.T) {
	registry := NewRegistry()
	w := &limitedWriter{limit: 3}
	session, err := registry.Register(domain.ProviderProfileOmniIoTV135, testIMEI, testDeviceID, testProjectID, "iot-one", w)
	if err != nil {
		t.Fatal(err)
	}
	frame := []byte("0123456789")
	result := registry.WriteUnique(context.Background(), session, frame)
	if !result.Complete || result.Err != nil || result.BytesWritten != len(frame) || !bytes.Equal(w.Bytes(), frame) {
		t.Fatalf("write result = %+v, body = %q", result, w.Bytes())
	}
}

func TestRegistryAppliesContextDeadlineToConnectionWriter(t *testing.T) {
	registry := NewRegistry()
	w := &deadlineWriter{}
	session, err := registry.Register(domain.ProviderProfileOmniIoTV135, testIMEI, testDeviceID, testProjectID, "iot-deadline", w)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second).UTC()
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()

	result := registry.WriteUnique(ctx, session, []byte("frame"))
	if !result.Complete || result.Err != nil || result.BytesWritten != len("frame") {
		t.Fatalf("write result = %+v", result)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.deadlines) != 2 || !w.deadlines[0].Equal(deadline) || !w.deadlines[1].IsZero() {
		t.Fatalf("write deadlines = %v, want context deadline followed by reset", w.deadlines)
	}
}

func TestRegistryRefusesSessionThatBecameAmbiguous(t *testing.T) {
	registry := NewRegistry()
	firstWriter := &bytes.Buffer{}
	first, err := registry.Register(domain.ProviderProfileOmniIoTV135, testIMEI, testDeviceID, testProjectID, "iot-one", firstWriter)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Register(domain.ProviderProfileOmniIoTV135, testIMEI, testDeviceID, testProjectID, "iot-two", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	result := registry.WriteUnique(context.Background(), first, []byte("frame"))
	if result.BytesWritten != 0 || !errors.Is(result.Err, ErrSessionUnavailable) || firstWriter.Len() != 0 {
		t.Fatalf("ambiguous write result = %+v, bytes = %d", result, firstWriter.Len())
	}
}

func TestRegistryBlockedWriteDoesNotBlockAnotherIdentity(t *testing.T) {
	registry := NewRegistry()
	blocked := &blockingWriter{started: make(chan struct{}), release: make(chan struct{})}
	first, err := registry.Register(domain.ProviderProfileOmniIoTV135, testIMEI, testDeviceID, testProjectID, "iot-a", blocked)
	if err != nil {
		t.Fatal(err)
	}
	writeDone := make(chan WriteResult, 1)
	go func() { writeDone <- registry.WriteUnique(context.Background(), first, []byte("frame-a")) }()
	select {
	case <-blocked.started:
	case <-time.After(time.Second):
		t.Fatal("first identity write did not start")
	}

	otherDone := make(chan error, 1)
	go func() {
		other, registerErr := registry.Register(
			domain.ProviderProfileOmniIoTV135, "123456789012346",
			"10000000-0000-0000-0000-000000000002", "20000000-0000-0000-0000-000000000002",
			"iot-b", &bytes.Buffer{},
		)
		if registerErr == nil {
			registry.Unregister(other)
		}
		otherDone <- registerErr
	}()
	select {
	case err := <-otherDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked identity A prevented identity B registration")
	}

	sameDone := make(chan error, 1)
	go func() {
		_, registerErr := registry.Register(
			domain.ProviderProfileOmniIoTV135, testIMEI, testDeviceID, testProjectID,
			"iot-a-duplicate", &bytes.Buffer{},
		)
		sameDone <- registerErr
	}()
	select {
	case err := <-sameDone:
		t.Fatalf("same identity registration bypassed active write: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(blocked.release)
	if result := <-writeDone; !result.Complete || result.Err != nil {
		t.Fatalf("blocked write result = %+v", result)
	}
	select {
	case err := <-sameDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("same identity registration did not resume")
	}
	if _, err := registry.LookupUnique(domain.ProviderProfileOmniIoTV135, testIMEI); !errors.Is(err, ErrSessionAmbiguous) {
		t.Fatalf("same identity lookup error = %v", err)
	}
}

func TestRegistryRejectsUnregisteredGeneration(t *testing.T) {
	registry := NewRegistry()
	oldWriter := &bytes.Buffer{}
	old, err := registry.Register(domain.ProviderProfileOmniBikeV207, testIMEI, testDeviceID, testProjectID, "old", oldWriter)
	if err != nil {
		t.Fatal(err)
	}
	registry.Unregister(old)
	current, err := registry.Register(
		domain.ProviderProfileOmniBikeV207, testIMEI,
		"10000000-0000-0000-0000-000000000002", "20000000-0000-0000-0000-000000000002",
		"current", &bytes.Buffer{},
	)
	if err != nil {
		t.Fatal(err)
	}
	result := registry.WriteUnique(context.Background(), old, []byte("stale"))
	if result.BytesWritten != 0 || !errors.Is(result.Err, ErrSessionUnavailable) || oldWriter.Len() != 0 {
		t.Fatalf("stale generation write = %+v body=%q", result, oldWriter.Bytes())
	}
	if got, lookupErr := registry.LookupUnique(domain.ProviderProfileOmniBikeV207, testIMEI); lookupErr != nil || got != current {
		t.Fatalf("current generation lookup = %p, %v", got, lookupErr)
	}
	if current.DeviceID() == old.DeviceID() || current.ProjectID() == old.ProjectID() {
		t.Fatalf("replacement session did not change ownership: old=%s/%s current=%s/%s",
			old.ProjectID(), old.DeviceID(), current.ProjectID(), current.DeviceID())
	}
}

type limitedWriter struct {
	mu    sync.Mutex
	limit int
	body  bytes.Buffer
}

func (w *limitedWriter) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	limit := min(w.limit, len(value))
	return w.body.Write(value[:limit])
}

func (w *limitedWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.body.Bytes()...)
}

type scriptedWriter struct {
	mu      sync.Mutex
	steps   []writeStep
	writes  int
	written bytes.Buffer
}

type blockingWriter struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

type deadlineWriter struct {
	mu        sync.Mutex
	deadlines []time.Time
}

func (w *deadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func (w *deadlineWriter) Write(value []byte) (int, error) {
	return len(value), nil
}

func (w *blockingWriter) Write(value []byte) (int, error) {
	w.once.Do(func() { close(w.started) })
	<-w.release
	return len(value), nil
}

type writeStep struct {
	count int
	err   error
}

func (w *scriptedWriter) Write(value []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.steps) == 0 {
		return 0, io.ErrNoProgress
	}
	step := w.steps[0]
	w.steps = w.steps[1:]
	w.writes++
	count := min(step.count, len(value))
	_, _ = w.written.Write(value[:count])
	return count, step.err
}

func (w *scriptedWriter) writeCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writes
}

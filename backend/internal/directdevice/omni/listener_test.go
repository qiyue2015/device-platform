package omni

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/provideradapter"
)

func TestListenerRegistersOnlyAfterValidFirstQ0(t *testing.T) {
	registry := NewRegistry()
	recorder := newListenerTestRecorder(domain.ProviderProfileOmniIoTV135)
	_, network, cancel := startListenerTestServer(t, domain.ProviderProfileOmniIoTV135, registry, recorder)
	defer cancel()

	connection := network.connect(t)
	defer connection.Close()
	q0 := []byte("*SCOR,OM," + testIMEI + ",Q0,412,80,28#\n")
	h0 := []byte("*SCOR,OM," + testIMEI + ",H0,0,412,28,80,0#\n")
	if _, err := connection.Write(append(q0, h0...)); err != nil {
		t.Fatal(err)
	}
	first := recorder.next(t)
	second := recorder.next(t)
	if !first.FirstFrame || first.ExpectedDeviceID != "" || first.Decoded.Frame.Command != "Q0" {
		t.Fatalf("first record = %+v", first)
	}
	if second.FirstFrame || second.ExpectedDeviceID != testDeviceID || second.Decoded.Frame.Command != "H0" {
		t.Fatalf("second record = %+v", second)
	}
	session, err := registry.LookupUnique(domain.ProviderProfileOmniIoTV135, testIMEI)
	if err != nil {
		t.Fatal(err)
	}
	if session.DeviceID() != testDeviceID || session.ProjectID() != testProjectID {
		t.Fatalf("session ownership = %s/%s", session.DeviceID(), session.ProjectID())
	}

	if err := connection.Close(); err != nil {
		t.Fatal(err)
	}
	assertEventually(t, func() bool {
		_, lookupErr := registry.LookupUnique(domain.ProviderProfileOmniIoTV135, testIMEI)
		return errors.Is(lookupErr, ErrSessionUnavailable)
	})
}

func TestListenerRejectsHeartbeatAsFirstFrame(t *testing.T) {
	registry := NewRegistry()
	recorder := newListenerTestRecorder(domain.ProviderProfileOmniBikeV207)
	_, network, cancel := startListenerTestServer(t, domain.ProviderProfileOmniBikeV207, registry, recorder)
	defer cancel()

	connection := network.connect(t)
	defer connection.Close()
	h0 := []byte("*CMDR,OM," + testIMEI + ",260801112233,H0,0,412,28#\n")
	if _, err := connection.Write(h0); err != nil {
		t.Fatal(err)
	}
	request := recorder.next(t)
	if !request.FirstFrame || request.Decoded.Frame.Command != "H0" {
		t.Fatalf("record = %+v", request)
	}
	assertConnectionClosed(t, connection)
	if _, err := registry.LookupUnique(domain.ProviderProfileOmniBikeV207, testIMEI); !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("heartbeat established session: %v", err)
	}
}

func TestListenerDoesNotInferProfileFromFirstFrame(t *testing.T) {
	registry := NewRegistry()
	recorder := newListenerTestRecorder(domain.ProviderProfileOmniBikeV207)
	_, network, cancel := startListenerTestServer(t, domain.ProviderProfileOmniBikeV207, registry, recorder)
	defer cancel()

	connection := network.connect(t)
	defer connection.Close()
	iotQ0 := []byte("*SCOR,OM," + testIMEI + ",Q0,412,80,28#\n")
	if _, err := connection.Write(iotQ0); err != nil {
		t.Fatal(err)
	}
	request := recorder.next(t)
	if ErrorCode(request.Decoded.Err) != FrameInvalidHeader || request.Profile != domain.ProviderProfileOmniBikeV207 {
		t.Fatalf("cross-profile record = %+v", request)
	}
	assertConnectionClosed(t, connection)
}

func TestListenerCancellationClosesBoundIdleConnectionAndUnregisters(t *testing.T) {
	registry := NewRegistry()
	recorder := newListenerTestRecorder(domain.ProviderProfileOmniIoTV135)
	_, network, stop := startListenerTestServer(t, domain.ProviderProfileOmniIoTV135, registry, recorder)

	connection := network.connect(t)
	q0 := []byte("*SCOR,OM," + testIMEI + ",Q0,412,80,28#\n")
	if _, err := connection.Write(q0); err != nil {
		t.Fatal(err)
	}
	_ = recorder.next(t)
	assertEventually(t, func() bool {
		_, lookupErr := registry.LookupUnique(domain.ProviderProfileOmniIoTV135, testIMEI)
		return lookupErr == nil
	})

	stop()
	assertConnectionClosed(t, connection)
	if _, err := registry.LookupUnique(domain.ProviderProfileOmniIoTV135, testIMEI); !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("cancelled listener retained session: %v", err)
	}
}

func TestListenerCancellationClosesIdleConnectionBeforeHandshake(t *testing.T) {
	registry := NewRegistry()
	recorder := newListenerTestRecorder(domain.ProviderProfileOmniBikeV207)
	_, network, stop := startListenerTestServer(t, domain.ProviderProfileOmniBikeV207, registry, recorder)
	connection := network.connect(t)

	stop()
	assertConnectionClosed(t, connection)
	select {
	case request := <-recorder.requests:
		t.Fatalf("idle connection created RawMessage: %+v", request)
	default:
	}
}

func TestListenerAcceptFailureCancelsBlockedRecorderBeforeWaiting(t *testing.T) {
	registry := NewRegistry()
	network := newPipeListener()
	recorder := &blockingListenerTestRecorder{started: make(chan struct{}), cancelled: make(chan struct{})}
	listener, err := NewTCPListener(network, ListenerConfig{
		Profile: domain.ProviderProfileOmniIoTV135, MaxFrameBytes: 256, MaxConnections: 4, ReadTimeout: time.Hour,
	}, registry, recorder)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- listener.Run(ctx, func(error) {}) }()

	connection := network.connect(t)
	defer connection.Close()
	if _, err := connection.Write([]byte("*SCOR,OM," + testIMEI + ",Q0,412,80,28#\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-recorder.started:
	case <-time.After(time.Second):
		t.Fatal("listener did not enter inbound persistence")
	}

	network.fail(errors.New("injected accept failure"))
	select {
	case runErr := <-done:
		if !errors.Is(runErr, ErrListenerAccept) {
			t.Fatalf("listener error = %v", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("listener waited forever for blocked inbound persistence")
	}
	select {
	case <-recorder.cancelled:
	default:
		t.Fatal("listener returned without cancelling inbound persistence")
	}
	assertConnectionClosed(t, connection)
}

func TestRuntimeAcceptFailureStopsSiblingAndDisablesDispatch(t *testing.T) {
	registry := NewRegistry()
	bikeNetwork := newPipeListener()
	iotNetwork := newPipeListener()
	bikeRecorder := newListenerTestRecorder(domain.ProviderProfileOmniBikeV207)
	bike, err := NewTCPListener(bikeNetwork, ListenerConfig{
		Profile: domain.ProviderProfileOmniBikeV207, MaxFrameBytes: 256, MaxConnections: 4, ReadTimeout: time.Hour,
	}, registry, bikeRecorder)
	if err != nil {
		t.Fatal(err)
	}
	iotRecorder := newListenerTestRecorder(domain.ProviderProfileOmniIoTV135)
	iot, err := NewTCPListener(iotNetwork, ListenerConfig{
		Profile: domain.ProviderProfileOmniIoTV135, MaxFrameBytes: 256, MaxConnections: 4, ReadTimeout: time.Hour,
	}, registry, iotRecorder)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &Runtime{registry: registry, listeners: []*TCPListener{bike, iot}, configured: true}
	runtime.healthy.Store(true)
	runtime.adapter = NewAdapter(registry, AdapterConfig{Configured: true, Available: runtime.Configured})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx, func(error) {}) }()

	bikeConnection := bikeNetwork.connect(t)
	defer bikeConnection.Close()
	if _, err := bikeConnection.Write([]byte("*CMDR,OM," + testIMEI + ",260801112233,Q0,412#\n")); err != nil {
		t.Fatal(err)
	}
	_ = bikeRecorder.next(t)
	iotConnection := iotNetwork.connect(t)
	defer iotConnection.Close()
	if _, err := iotConnection.Write([]byte("*SCOR,OM," + testIMEI + ",Q0,412,80,28#\n")); err != nil {
		t.Fatal(err)
	}
	_ = iotRecorder.next(t)
	assertEventually(t, func() bool {
		_, bikeErr := registry.LookupUnique(domain.ProviderProfileOmniBikeV207, testIMEI)
		_, iotErr := registry.LookupUnique(domain.ProviderProfileOmniIoTV135, testIMEI)
		return bikeErr == nil && iotErr == nil
	})

	bikeNetwork.fail(errors.New("injected accept failure"))
	select {
	case runErr := <-done:
		if !errors.Is(runErr, ErrRuntimeDegraded) || !errors.Is(runErr, ErrListenerAccept) {
			t.Fatalf("runtime error = %v", runErr)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop after listener failure")
	}

	assertConnectionClosed(t, bikeConnection)
	assertConnectionClosed(t, iotConnection)
	if _, err := registry.LookupUnique(domain.ProviderProfileOmniBikeV207, testIMEI); !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("failed runtime retained failed-listener session: %v", err)
	}
	if _, err := registry.LookupUnique(domain.ProviderProfileOmniIoTV135, testIMEI); !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("failed runtime retained sibling session: %v", err)
	}
	if runtime.Configured() || runtime.Adapter().Configured() {
		t.Fatal("failed dual-profile runtime remained configured")
	}
	_, err = runtime.Adapter().Prepare(provideradapter.DispatchRequest{
		ProjectID: testProjectID, DeviceID: testDeviceID,
		ProviderDeviceID: testIMEI, ProviderProfile: domain.ProviderProfileOmniIoTV135,
		Action: "query_status", ProviderRequestKey: "attempt-after-runtime-failure",
	})
	var prepareErr *provideradapter.PrepareError
	if !errors.As(err, &prepareErr) || prepareErr.Failure != provideradapter.PrepareSessionUnavailable {
		t.Fatalf("Prepare after runtime failure = %v", err)
	}
}

type listenerTestRecorder struct {
	profile  string
	device   domain.Device
	requests chan InboundRecordRequest
	mu       sync.Mutex
	nextID   int
}

type blockingListenerTestRecorder struct {
	started    chan struct{}
	cancelled  chan struct{}
	startOnce  sync.Once
	cancelOnce sync.Once
}

func (r *blockingListenerTestRecorder) Record(ctx context.Context, _ InboundRecordRequest) (InboundRecordResult, error) {
	r.startOnce.Do(func() { close(r.started) })
	<-ctx.Done()
	r.cancelOnce.Do(func() { close(r.cancelled) })
	return InboundRecordResult{}, ctx.Err()
}

func newListenerTestRecorder(profile string) *listenerTestRecorder {
	return &listenerTestRecorder{
		profile: profile,
		device: domain.Device{
			ID: testDeviceID, ProjectID: testProjectID, DeviceTypeCode: domain.DeviceTypeSmartLock,
			ProviderCode: domain.ProviderCodeOmni, ProviderProfile: profile, ProviderDeviceID: testIMEI,
			AccessType: domain.AccessTypeDirectDevice, TransportProtocol: domain.TransportProtocolTCP,
			Adapter: domain.AdapterOmniDirectTCP, LifecycleStatus: domain.LifecycleStatusActive,
		},
		requests: make(chan InboundRecordRequest, 8),
	}
}

func (r *listenerTestRecorder) Record(_ context.Context, request InboundRecordRequest) (InboundRecordResult, error) {
	r.requests <- request
	r.mu.Lock()
	r.nextID++
	id := r.nextID
	r.mu.Unlock()
	result := InboundRecordResult{RawMessageID: "raw-message-" + strconv.Itoa(id)}
	if request.Profile != r.profile || request.Decoded.Err != nil || request.Decoded.Frame.IMEI != testIMEI {
		result.RejectCode = "invalid_frame"
		return result, nil
	}
	if request.FirstFrame && request.Decoded.Frame.Command != "Q0" {
		result.RejectCode = "handshake_required"
		return result, nil
	}
	if !request.FirstFrame && (request.Decoded.Frame.Command == "Q0" || request.ExpectedDeviceID != testDeviceID) {
		result.RejectCode = "session_identity_mismatch"
		return result, nil
	}
	device := r.device
	result.Accepted = true
	result.Device = &device
	return result, nil
}

func (r *listenerTestRecorder) next(t *testing.T) InboundRecordRequest {
	t.Helper()
	select {
	case request := <-r.requests:
		return request
	case <-time.After(time.Second):
		t.Fatal("listener did not record frame")
		return InboundRecordRequest{}
	}
}

func startListenerTestServer(t *testing.T, profile string, registry *Registry, recorder InboundRecorder) (*TCPListener, *pipeListener, context.CancelFunc) {
	t.Helper()
	networkListener := newPipeListener()
	listener, err := NewTCPListener(networkListener, ListenerConfig{
		Profile: profile, MaxFrameBytes: 256, MaxConnections: 4, ReadTimeout: time.Second,
	}, registry, recorder)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		listener.Run(ctx, func(error) {})
	}()
	var stopOnce sync.Once
	return listener, networkListener, func() {
		stopOnce.Do(func() {
			cancel()
			_ = listener.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("listener Run did not stop after cancellation")
			}
		})
	}
}

func assertConnectionClosed(t *testing.T, connection net.Conn) {
	t.Helper()
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return
	}
	var buffer [1]byte
	if _, err := connection.Read(buffer[:]); err == nil {
		t.Fatal("connection remained open after rejected identity")
	}
}

type pipeListener struct {
	connections chan net.Conn
	failures    chan error
	closed      chan struct{}
	closeOnce   sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{connections: make(chan net.Conn), failures: make(chan error, 1), closed: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case connection := <-l.connections:
		return connection, nil
	case err := <-l.failures:
		return nil, err
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddress("omni-test") }

func (l *pipeListener) fail(err error) {
	l.failures <- err
}

func (l *pipeListener) connect(t *testing.T) net.Conn {
	t.Helper()
	server, client := net.Pipe()
	select {
	case l.connections <- server:
		return client
	case <-l.closed:
		_ = server.Close()
		_ = client.Close()
		t.Fatal("listener is closed")
		return nil
	case <-time.After(time.Second):
		_ = server.Close()
		_ = client.Close()
		t.Fatal("listener did not accept connection")
		return nil
	}
}

type pipeAddress string

func (a pipeAddress) Network() string { return "pipe" }
func (a pipeAddress) String() string  { return string(a) }

func assertEventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not satisfied")
}

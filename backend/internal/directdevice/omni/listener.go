package omni

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/qiyue2015/device-platform/internal/domain"
	"github.com/qiyue2015/device-platform/internal/storage/repository"
)

const (
	defaultMaxFrameBytes  = 4096
	defaultMaxConnections = 256
	defaultReadTimeout    = 5 * time.Minute
)

var (
	ErrListenerAccept       = errors.New("omni listener accept failed")
	ErrConnectionLimit      = errors.New("omni listener connection limit reached")
	ErrInboundRecord        = errors.New("omni inbound persistence failed")
	ErrConnectionIdentity   = errors.New("omni connection identity rejected")
	ErrConnectionIdentifier = errors.New("omni connection identifier generation failed")
	ErrRuntimeDegraded      = errors.New("omni dual-profile runtime degraded")
)

type ListenerConfig struct {
	Profile        string
	Address        string
	MaxFrameBytes  int
	MaxConnections int
	ReadTimeout    time.Duration
	Random         io.Reader
}

type TCPListener struct {
	profile        string
	listener       net.Listener
	registry       *Registry
	recorder       InboundRecorder
	maxFrameBytes  int
	maxConnections int
	readTimeout    time.Duration
	random         io.Reader
	randomMu       sync.Mutex
	closeOnce      sync.Once
	closeErr       error
	activeMu       sync.Mutex
	active         map[net.Conn]struct{}
	closing        bool
	connections    chan struct{}
}

func OpenTCPListener(config ListenerConfig, registry *Registry, recorder InboundRecorder) (*TCPListener, error) {
	if !validProfile(config.Profile) || strings.TrimSpace(config.Address) == "" || registry == nil || recorder == nil {
		return nil, fmt.Errorf("invalid Omni listener configuration")
	}
	listener, err := net.Listen("tcp", config.Address)
	if err != nil {
		return nil, fmt.Errorf("open Omni listener for profile %s", config.Profile)
	}
	server, err := NewTCPListener(listener, config, registry, recorder)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	return server, nil
}

func NewTCPListener(listener net.Listener, config ListenerConfig, registry *Registry, recorder InboundRecorder) (*TCPListener, error) {
	if listener == nil || !validProfile(config.Profile) || registry == nil || recorder == nil {
		return nil, fmt.Errorf("invalid Omni listener configuration")
	}
	if config.MaxFrameBytes == 0 {
		config.MaxFrameBytes = defaultMaxFrameBytes
	}
	if config.MaxConnections == 0 {
		config.MaxConnections = defaultMaxConnections
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = defaultReadTimeout
	}
	if config.MaxFrameBytes < 64 || config.MaxConnections < 1 || config.ReadTimeout <= 0 {
		return nil, fmt.Errorf("invalid Omni listener limits")
	}
	if config.Random == nil {
		config.Random = rand.Reader
	}
	return &TCPListener{
		profile: config.Profile, listener: listener, registry: registry, recorder: recorder,
		maxFrameBytes: config.MaxFrameBytes, maxConnections: config.MaxConnections,
		readTimeout: config.ReadTimeout, random: config.Random,
		active:      make(map[net.Conn]struct{}),
		connections: make(chan struct{}, config.MaxConnections),
	}, nil
}

func (l *TCPListener) Address() string {
	if l == nil || l.listener == nil {
		return ""
	}
	return l.listener.Addr().String()
}

func (l *TCPListener) Run(ctx context.Context, report func(error)) error {
	if l == nil || l.listener == nil {
		return ErrListenerAccept
	}
	if report == nil {
		report = func(error) {}
	}
	handlerCtx, cancelHandlers := context.WithCancel(ctx)
	go func() {
		<-handlerCtx.Done()
		_ = l.Close()
	}()
	var group sync.WaitGroup
	defer func() {
		cancelHandlers()
		_ = l.Close()
		group.Wait()
	}()
	for {
		connection, err := l.listener.Accept()
		if err != nil {
			if ctx.Err() == nil {
				report(ErrListenerAccept)
				return ErrListenerAccept
			}
			return nil
		}
		select {
		case l.connections <- struct{}{}:
			if !l.track(connection) {
				<-l.connections
				_ = connection.Close()
				continue
			}
			group.Add(1)
			go func() {
				defer group.Done()
				defer func() { <-l.connections }()
				defer l.untrack(connection)
				l.handleConnection(handlerCtx, connection, report)
			}()
		default:
			_ = connection.Close()
			report(ErrConnectionLimit)
		}
	}
}

func (l *TCPListener) Close() error {
	if l == nil || l.listener == nil {
		return nil
	}
	l.closeOnce.Do(func() {
		l.activeMu.Lock()
		l.closing = true
		connections := make([]net.Conn, 0, len(l.active))
		for connection := range l.active {
			connections = append(connections, connection)
		}
		l.activeMu.Unlock()

		errs := make([]error, 0, len(connections)+1)
		if err := l.listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
		for _, connection := range connections {
			if err := connection.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				errs = append(errs, err)
			}
		}
		l.closeErr = errors.Join(errs...)
	})
	return l.closeErr
}

func (l *TCPListener) track(connection net.Conn) bool {
	l.activeMu.Lock()
	defer l.activeMu.Unlock()
	if l.closing {
		return false
	}
	l.active[connection] = struct{}{}
	return true
}

func (l *TCPListener) untrack(connection net.Conn) {
	l.activeMu.Lock()
	delete(l.active, connection)
	l.activeMu.Unlock()
}

func (l *TCPListener) handleConnection(ctx context.Context, connection net.Conn, report func(error)) {
	defer connection.Close()
	connectionID, err := l.connectionID()
	if err != nil {
		report(ErrConnectionIdentifier)
		return
	}
	decoder, err := NewDecoder(l.profile, l.maxFrameBytes)
	if err != nil {
		return
	}
	buffer := make([]byte, min(l.maxFrameBytes, 4096))
	firstFrame := true
	var boundDevice *domain.Device
	var session *Session
	defer func() { l.registry.Unregister(session) }()

	process := func(decoded DecodedFrame) bool {
		expectedDeviceID := ""
		if boundDevice != nil {
			expectedDeviceID = boundDevice.ID
		}
		result, recordErr := l.recorder.Record(ctx, InboundRecordRequest{
			Profile: l.profile, ConnectionID: connectionID, FirstFrame: firstFrame,
			ExpectedDeviceID: expectedDeviceID, Decoded: decoded,
		})
		if recordErr != nil {
			report(ErrInboundRecord)
			return false
		}
		if !result.Accepted || result.Device == nil {
			report(ErrConnectionIdentity)
			return false
		}
		if firstFrame {
			registered, registerErr := l.registry.Register(
				l.profile, result.Device.ProviderDeviceID, result.Device.ID, result.Device.ProjectID,
				connectionID, connection,
			)
			if registerErr != nil {
				report(ErrConnectionIdentity)
				return false
			}
			session = registered
			device := *result.Device
			boundDevice = &device
			firstFrame = false
		}
		return true
	}

	for {
		if err := connection.SetReadDeadline(time.Now().Add(l.readTimeout)); err != nil {
			return
		}
		count, readErr := connection.Read(buffer)
		if count > 0 {
			for _, decoded := range decoder.Feed(buffer[:count]) {
				if !process(decoded) {
					return
				}
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				for _, decoded := range decoder.Close() {
					_ = process(decoded)
				}
			}
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (l *TCPListener) connectionID() (string, error) {
	l.randomMu.Lock()
	defer l.randomMu.Unlock()
	return inboundUUID(l.random)
}

type RuntimeConfig struct {
	BikeAddress      string
	IoTAddress       string
	MaxFrameBytes    int
	MaxConnections   int
	ReadTimeout      time.Duration
	AdapterClock     Clock
	ConnectionRandom io.Reader
	RecordRandom     io.Reader
}

type Runtime struct {
	registry   *Registry
	adapter    *Adapter
	listeners  []*TCPListener
	configured bool
	healthy    atomic.Bool
}

func OpenRuntime(store repository.ProviderMessageStore, config RuntimeConfig) (*Runtime, error) {
	bikeAddress := strings.TrimSpace(config.BikeAddress)
	iotAddress := strings.TrimSpace(config.IoTAddress)
	if (bikeAddress == "") != (iotAddress == "") {
		return nil, fmt.Errorf("both Omni profile listener addresses are required")
	}
	registry := NewRegistry()
	runtime := &Runtime{registry: registry, configured: bikeAddress != ""}
	runtime.healthy.Store(runtime.configured)
	runtime.adapter = NewAdapter(registry, AdapterConfig{
		Configured: runtime.configured, Clock: config.AdapterClock, Available: runtime.Configured,
	})
	if bikeAddress == "" {
		return runtime, nil
	}
	recorder, err := NewPersistentInboundRecorder(store, InboundRecorderConfig{
		Random: config.RecordRandom, Clock: config.AdapterClock,
	})
	if err != nil {
		return nil, err
	}
	profiles := []struct {
		profile string
		address string
	}{
		{profile: domain.ProviderProfileOmniBikeV207, address: bikeAddress},
		{profile: domain.ProviderProfileOmniIoTV135, address: iotAddress},
	}
	for _, item := range profiles {
		listener, openErr := OpenTCPListener(ListenerConfig{
			Profile: item.profile, Address: item.address, MaxFrameBytes: config.MaxFrameBytes,
			MaxConnections: config.MaxConnections, ReadTimeout: config.ReadTimeout,
			Random: config.ConnectionRandom,
		}, registry, recorder)
		if openErr != nil {
			_ = runtime.Close()
			return nil, openErr
		}
		runtime.listeners = append(runtime.listeners, listener)
	}
	return runtime, nil
}

func (r *Runtime) Adapter() *Adapter {
	if r == nil {
		return nil
	}
	return r.adapter
}

func (r *Runtime) Registry() *Registry {
	if r == nil {
		return nil
	}
	return r.registry
}

func (r *Runtime) Configured() bool {
	return r != nil && r.configured && len(r.listeners) == 2 && r.healthy.Load()
}

func (r *Runtime) Addresses() map[string]string {
	result := make(map[string]string, len(r.listeners))
	for _, listener := range r.listeners {
		result[listener.profile] = listener.Address()
	}
	return result
}

func (r *Runtime) Run(ctx context.Context, report func(error)) error {
	if r == nil || !r.configured || len(r.listeners) == 0 {
		<-ctx.Done()
		return nil
	}
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type listenerResult struct{ err error }
	results := make(chan listenerResult, len(r.listeners))
	for _, listener := range r.listeners {
		go func(listener *TCPListener) {
			results <- listenerResult{err: listener.Run(childCtx, report)}
		}(listener)
	}
	first := <-results
	unexpected := first.err != nil || ctx.Err() == nil
	r.healthy.Store(false)
	cancel()
	_ = r.Close()
	for range len(r.listeners) - 1 {
		<-results
	}
	if unexpected {
		if first.err != nil {
			return errors.Join(ErrRuntimeDegraded, first.err)
		}
		return ErrRuntimeDegraded
	}
	return nil
}

func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	r.healthy.Store(false)
	var errs []error
	for _, listener := range r.listeners {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

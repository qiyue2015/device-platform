package gateway

import (
	"context"
	"sync"
	"time"
)

const (
	defaultDelay         = 150 * time.Millisecond
	defaultTimeoutOffset = 50 * time.Millisecond
	defaultHeartbeat     = 100 * time.Millisecond
)

type SimulatorGateway struct {
	mu            sync.RWMutex
	mode          Mode
	delay         time.Duration
	timeoutOffset time.Duration
	heartbeat     time.Duration
	online        bool
	lastHeartbeat time.Time

	results    chan CommandResult
	heartbeats chan Heartbeat
	stop       chan struct{}
	wake       chan struct{}
	once       sync.Once
	now        func() time.Time
}

func NewSimulatorGateway(config ModeConfig) *SimulatorGateway {
	g := &SimulatorGateway{
		mode:          ModeNormal,
		delay:         defaultDelay,
		timeoutOffset: defaultTimeoutOffset,
		heartbeat:     defaultHeartbeat,
		online:        true,
		results:       make(chan CommandResult, 64),
		heartbeats:    make(chan Heartbeat, 64),
		stop:          make(chan struct{}),
		wake:          make(chan struct{}, 1),
		now:           time.Now,
	}
	_ = g.SetMode(config)
	go g.heartbeatLoop()
	return g
}

func (g *SimulatorGateway) Dispatch(ctx context.Context, command Command) error {
	g.mu.RLock()
	mode := g.mode
	delay := g.delay
	timeoutOffset := g.timeoutOffset
	online := g.online
	g.mu.RUnlock()

	if mode == ModeOffline || !online {
		return nil
	}

	switch mode {
	case ModeNormal:
		g.scheduleResult(ctx, command, 0, "success", "", false)
	case ModeDelay:
		g.scheduleResult(ctx, command, delay, "success", "", false)
	case ModeTimeoutThenAck:
		wait := command.Timeout + timeoutOffset
		if wait < 0 {
			wait = 0
		}
		g.scheduleResult(ctx, command, wait, "success", "", true)
	case ModeDuplicateAck:
		g.scheduleResult(ctx, command, 0, "success", "", false)
		g.scheduleResult(ctx, command, 10*time.Millisecond, "success", "", false)
	case ModeFail:
		g.scheduleResult(ctx, command, 0, "failed", "simulator_failed", false)
	}
	return nil
}

func (g *SimulatorGateway) SetMode(config ModeConfig) error {
	mode := config.Mode
	if mode == "" {
		mode = ModeNormal
	}
	if _, ok := validModes[mode]; !ok {
		return ErrInvalidMode
	}

	g.mu.Lock()
	g.mode = mode
	if config.Delay > 0 {
		g.delay = config.Delay
	}
	if config.TimeoutOffset > 0 {
		g.timeoutOffset = config.TimeoutOffset
	}
	if config.Heartbeat > 0 {
		g.heartbeat = config.Heartbeat
	}
	g.online = mode != ModeOffline
	if g.online {
		g.lastHeartbeat = g.now()
	}
	g.mu.Unlock()

	g.signalWake()
	return nil
}

func (g *SimulatorGateway) Snapshot() Snapshot {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return Snapshot{
		Mode:          g.mode,
		Delay:         g.delay,
		TimeoutOffset: g.timeoutOffset,
		Heartbeat:     g.heartbeat,
		Online:        g.online,
		LastHeartbeat: g.lastHeartbeat,
	}
}

func (g *SimulatorGateway) SubscribeResults() <-chan CommandResult {
	return g.results
}

func (g *SimulatorGateway) SubscribeHeartbeats() <-chan Heartbeat {
	return g.heartbeats
}

func (g *SimulatorGateway) Stop() {
	g.once.Do(func() {
		close(g.stop)
	})
}

func (g *SimulatorGateway) scheduleResult(ctx context.Context, command Command, wait time.Duration, status string, reason string, corrected bool) {
	go func() {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-g.stop:
			return
		case <-timer.C:
		}

		g.publishResult(CommandResult{
			CommandID: command.ID,
			Status:    status,
			Reason:    reason,
			Corrected: corrected,
			At:        g.now(),
		})
	}()
}

func (g *SimulatorGateway) publishResult(result CommandResult) {
	select {
	case <-g.stop:
		return
	case g.results <- result:
	default:
	}
}

func (g *SimulatorGateway) heartbeatLoop() {
	for {
		g.mu.RLock()
		interval := g.heartbeat
		g.mu.RUnlock()
		if interval <= 0 {
			interval = defaultHeartbeat
		}

		timer := time.NewTimer(interval)
		select {
		case <-g.stop:
			timer.Stop()
			return
		case <-g.wake:
			timer.Stop()
			g.emitHeartbeatIfOnline()
		case <-timer.C:
			g.emitHeartbeatIfOnline()
		}
	}
}

func (g *SimulatorGateway) emitHeartbeatIfOnline() {
	g.mu.Lock()
	if !g.online {
		g.mu.Unlock()
		return
	}
	g.lastHeartbeat = g.now()
	hb := Heartbeat{DeviceID: "simulator", At: g.lastHeartbeat, Online: true}
	g.mu.Unlock()

	select {
	case <-g.stop:
	case g.heartbeats <- hb:
	default:
	}
}

func (g *SimulatorGateway) signalWake() {
	select {
	case g.wake <- struct{}{}:
	default:
	}
}

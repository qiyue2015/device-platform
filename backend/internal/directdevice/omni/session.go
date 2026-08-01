package omni

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

var (
	ErrSessionUnavailable = errors.New("omni session is unavailable")
	ErrSessionAmbiguous   = errors.New("omni session identity is ambiguous")
	ErrSessionInvalid     = errors.New("omni session registration is invalid")
)

type writer interface {
	Write([]byte) (int, error)
}

type writeDeadlineSetter interface {
	SetWriteDeadline(time.Time) error
}

type sessionKey struct {
	profile string
	imei    string
}

type Session struct {
	key          sessionKey
	deviceID     string
	projectID    string
	connectionID string
	generation   uint64
	writer       writer
	writeMu      sync.Mutex
}

func (s *Session) Profile() string      { return s.key.profile }
func (s *Session) IMEI() string         { return s.key.imei }
func (s *Session) DeviceID() string     { return s.deviceID }
func (s *Session) ProjectID() string    { return s.projectID }
func (s *Session) ConnectionID() string { return s.connectionID }
func (s *Session) Generation() uint64   { return s.generation }

type Registry struct {
	mu         sync.RWMutex
	identities map[sessionKey]*identitySessions
}

type identitySessions struct {
	mu         sync.RWMutex
	sessions   map[uint64]*Session
	generation uint64
}

func NewRegistry() *Registry {
	return &Registry{
		identities: make(map[sessionKey]*identitySessions),
	}
}

func (r *Registry) Register(profile, imei, deviceID, projectID, connectionID string, destination writer) (*Session, error) {
	if r == nil || !validProfile(profile) || !validIMEI(imei) || deviceID == "" || projectID == "" ||
		connectionID == "" || destination == nil {
		return nil, ErrSessionInvalid
	}
	key := sessionKey{profile: profile, imei: imei}
	identity := r.identity(key, true)
	identity.mu.Lock()
	defer identity.mu.Unlock()
	identity.generation++
	session := &Session{
		key: key, deviceID: deviceID, projectID: projectID,
		connectionID: connectionID, generation: identity.generation, writer: destination,
	}
	identity.sessions[session.generation] = session
	return session, nil
}

func (r *Registry) Unregister(session *Session) {
	if r == nil || session == nil {
		return
	}
	identity := r.identity(session.key, false)
	if identity == nil {
		return
	}
	identity.mu.Lock()
	defer identity.mu.Unlock()
	if identity.sessions[session.generation] != session {
		return
	}
	delete(identity.sessions, session.generation)
}

func (r *Registry) LookupUnique(profile, imei string) (*Session, error) {
	if r == nil || !validProfile(profile) || !validIMEI(imei) {
		return nil, ErrSessionUnavailable
	}
	identity := r.identity(sessionKey{profile: profile, imei: imei}, false)
	if identity == nil {
		return nil, ErrSessionUnavailable
	}
	identity.mu.RLock()
	defer identity.mu.RUnlock()
	if len(identity.sessions) == 0 {
		return nil, ErrSessionUnavailable
	}
	if len(identity.sessions) != 1 {
		return nil, ErrSessionAmbiguous
	}
	for _, session := range identity.sessions {
		return session, nil
	}
	return nil, ErrSessionUnavailable
}

type WriteResult struct {
	BytesWritten int
	Complete     bool
	Err          error
}

func (r *Registry) WriteUnique(ctx context.Context, session *Session, frame []byte) WriteResult {
	if r == nil || session == nil || len(frame) == 0 {
		return WriteResult{Err: ErrSessionUnavailable}
	}
	identity := r.identity(session.key, false)
	if identity == nil {
		return WriteResult{Err: ErrSessionUnavailable}
	}
	identity.mu.RLock()
	defer identity.mu.RUnlock()
	if len(identity.sessions) != 1 || identity.sessions[session.generation] != session {
		return WriteResult{Err: ErrSessionUnavailable}
	}

	session.writeMu.Lock()
	defer session.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return WriteResult{Err: err}
	}
	if deadlineWriter, ok := session.writer.(writeDeadlineSetter); ok {
		deadline, hasDeadline := ctx.Deadline()
		if hasDeadline {
			if err := deadlineWriter.SetWriteDeadline(deadline); err != nil {
				return WriteResult{Err: err}
			}
			defer func() { _ = deadlineWriter.SetWriteDeadline(time.Time{}) }()
		}
	}

	written := 0
	for written < len(frame) {
		if err := ctx.Err(); err != nil {
			return WriteResult{BytesWritten: written, Err: err}
		}
		count, err := session.writer.Write(frame[written:])
		if count < 0 || count > len(frame)-written {
			return WriteResult{BytesWritten: written, Err: fmt.Errorf("invalid Omni writer count")}
		}
		written += count
		if written == len(frame) {
			return WriteResult{BytesWritten: written, Complete: true}
		}
		if err != nil {
			return WriteResult{BytesWritten: written, Err: err}
		}
		if count == 0 {
			return WriteResult{BytesWritten: written, Err: io.ErrNoProgress}
		}
	}
	return WriteResult{BytesWritten: written, Complete: true}
}

func (r *Registry) identity(key sessionKey, create bool) *identitySessions {
	r.mu.RLock()
	identity := r.identities[key]
	r.mu.RUnlock()
	if identity != nil || !create {
		return identity
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if identity = r.identities[key]; identity == nil {
		identity = &identitySessions{sessions: make(map[uint64]*Session)}
		r.identities[key] = identity
	}
	return identity
}

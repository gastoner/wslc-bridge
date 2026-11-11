package execstore

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

type State string

const (
	Created State = "created"
	Running State = "running"
	Exited  State = "exited"
	Failed  State = "failed"
)

const (
	maxRecords = 1024
	maxAge     = 15 * time.Minute
)

// FailedExitCode is reported for an exec that never produced an exit status, so
// clients never mistake a failed exec for a successful one.
const FailedExitCode = -1

type Record struct {
	ID          string     `json:"ID"`
	ContainerID string     `json:"ContainerID"`
	Cmd         []string   `json:"ProcessConfig,omitempty"`
	State       State      `json:"-"`
	ExitCode    *int       `json:"ExitCode,omitempty"`
	Failure     string     `json:"-"`
	CreatedAt   time.Time  `json:"-"`
	FinishedAt  *time.Time `json:"-"`
}

type Store struct {
	mu      sync.Mutex
	records map[string]Record
}

func New() *Store {
	return &Store{records: make(map[string]Record)}
}

func (s *Store) Create(containerID string, command []string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	if len(s.records) >= maxRecords {
		return Record{}, errors.New("exec record capacity is full")
	}
	identifier, err := newID()
	if err != nil {
		return Record{}, err
	}
	record := Record{ID: identifier, ContainerID: containerID, Cmd: append([]string(nil), command...), State: Created, CreatedAt: time.Now()}
	s.records[identifier] = record
	return record, nil
}

func (s *Store) Start(id string) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return Record{}, errors.New("exec instance not found")
	}
	if record.State != Created {
		return Record{}, errors.New("exec instance has already been started")
	}
	record.State = Running
	s.records[id] = record
	return record, nil
}

func (s *Store) Complete(id string, exitCode int) (Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return Record{}, errors.New("exec instance not found")
	}
	now := time.Now()
	record.State = Exited
	record.ExitCode = &exitCode
	record.FinishedAt = &now
	s.records[id] = record
	return record, nil
}

func (s *Store) Fail(id string, cause error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[id]
	if !ok {
		return
	}
	exitCode := FailedExitCode
	now := time.Now()
	record.State = Failed
	record.ExitCode = &exitCode
	record.FinishedAt = &now
	if cause != nil {
		record.Failure = cause.Error()
	}
	s.records[id] = record
}

func (s *Store) Get(id string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	record, ok := s.records[id]
	return record, ok
}

func (s *Store) pruneLocked(now time.Time) {
	for id, record := range s.records {
		if record.State != Running && now.Sub(record.CreatedAt) > maxAge {
			delete(s.records, id)
		}
	}
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

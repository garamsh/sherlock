package gateway_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/gagent/internal/agent"
	"github.com/garamsh/gagent/internal/environment"
	"github.com/garamsh/gagent/internal/gateway"
)

// fakeFactory builds stub runtimes and records which environments were
// created.
type fakeFactory struct {
	mu      sync.Mutex
	created []string
	newFn   func(envID string, events agent.EventSink) (*gateway.Runtime, error)
}

func (f *fakeFactory) New(_ context.Context, envID string, events agent.EventSink) (*gateway.Runtime, error) {
	f.mu.Lock()
	f.created = append(f.created, envID)
	f.mu.Unlock()
	return f.newFn(envID, events)
}

func (f *fakeFactory) createdEnvs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.created...)
}

// fakeSession records the order tasks run in. A non-nil gate blocks every
// run until the gate closes (or the run context is canceled).
type fakeSession struct {
	gate <-chan struct{}

	mu      sync.Mutex
	started []string
	failed  []string
	err     error
}

func (s *fakeSession) Run(ctx context.Context, task string) (string, error) {
	s.mu.Lock()
	s.started = append(s.started, task)
	s.mu.Unlock()
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	if s.err != nil {
		s.mu.Lock()
		s.failed = append(s.failed, task)
		s.mu.Unlock()
		return "", s.err
	}
	return "done", nil
}

func (s *fakeSession) History() []agent.Message { return nil }

func (s *fakeSession) startedTasks() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.started...)
}

func newTestLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestRouter_EnqueueAssignsIDAndRunsMessage(t *testing.T) {
	session := &fakeSession{}
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: session, Close: func(context.Context) error { return nil }}, nil
	}}
	router := gateway.NewRouter(factory, newTestLogger())
	defer router.Close()

	id, err := router.Enqueue(context.Background(), "env-1", "hello")
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 1
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, []string{"hello"}, session.startedTasks())
}

func TestRouter_SerializesRunsFIFO(t *testing.T) {
	gate := make(chan struct{})
	session := &fakeSession{gate: gate}
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: session, Close: func(context.Context) error { return nil }}, nil
	}}
	router := gateway.NewRouter(factory, newTestLogger())
	defer router.Close()

	_, err := router.Enqueue(context.Background(), "env-1", "first")
	require.NoError(t, err)
	_, err = router.Enqueue(context.Background(), "env-1", "second")
	require.NoError(t, err)

	// The first run starts and blocks; the second must not begin
	// mid-run — one active run per environment.
	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 1
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, []string{"first"}, session.startedTasks())

	close(gate)
	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 2
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, []string{"first", "second"}, session.startedTasks())
}

func TestRouter_CreatesEnvironmentOnce(t *testing.T) {
	session := &fakeSession{}
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: session, Close: func(context.Context) error { return nil }}, nil
	}}
	router := gateway.NewRouter(factory, newTestLogger())
	defer router.Close()

	for range 3 {
		_, err := router.Enqueue(context.Background(), "env-1", "hi")
		require.NoError(t, err)
	}
	assert.Equal(t, []string{"env-1"}, factory.createdEnvs())
}

func TestRouter_FactoryErrorSurfaces(t *testing.T) {
	factory := &fakeFactory{newFn: func(envID string, _ agent.EventSink) (*gateway.Runtime, error) {
		return nil, fmt.Errorf("%w: %q", environment.ErrInvalidID, envID)
	}}
	router := gateway.NewRouter(factory, newTestLogger())
	defer router.Close()

	_, err := router.Enqueue(context.Background(), "bad", "hi")
	assert.ErrorIs(t, err, environment.ErrInvalidID)
}

func TestRouter_EnqueueAfterCloseFails(t *testing.T) {
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: &fakeSession{}, Close: func(context.Context) error { return nil }}, nil
	}}
	router := gateway.NewRouter(factory, newTestLogger())
	require.NoError(t, router.Close())

	_, err := router.Enqueue(context.Background(), "env-1", "hi")
	assert.ErrorIs(t, err, gateway.ErrRouterClosed)
}

func TestRouter_CloseReleasesRuntimes(t *testing.T) {
	var mu sync.Mutex
	var closed []string
	factory := &fakeFactory{newFn: func(envID string, _ agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{
			Session: &fakeSession{},
			Close: func(context.Context) error {
				mu.Lock()
				closed = append(closed, envID)
				mu.Unlock()
				return nil
			},
		}, nil
	}}
	router := gateway.NewRouter(factory, newTestLogger())
	for _, envID := range []string{"env-1", "env-2"} {
		_, err := router.Enqueue(context.Background(), envID, "hi")
		require.NoError(t, err)
	}

	require.NoError(t, router.Close())
	mu.Lock()
	defer mu.Unlock()
	assert.ElementsMatch(t, []string{"env-1", "env-2"}, closed)
}

func TestRouter_FailedRunDoesNotBlockQueue(t *testing.T) {
	session := &fakeSession{err: errors.New("boom")}
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: session, Close: func(context.Context) error { return nil }}, nil
	}}
	router := gateway.NewRouter(factory, newTestLogger())
	defer router.Close()

	_, err := router.Enqueue(context.Background(), "env-1", "first")
	require.NoError(t, err)
	_, err = router.Enqueue(context.Background(), "env-1", "second")
	require.NoError(t, err)

	// Both messages run even though every turn fails.
	require.Eventually(t, func() bool {
		return len(session.startedTasks()) == 2
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, []string{"first", "second"}, session.startedTasks())
}

func TestRouter_EventsDoesNotCreateEnvironment(t *testing.T) {
	factory := &fakeFactory{newFn: func(string, agent.EventSink) (*gateway.Runtime, error) {
		return &gateway.Runtime{Session: &fakeSession{}, Close: func(context.Context) error { return nil }}, nil
	}}
	router := gateway.NewRouter(factory, newTestLogger())
	defer router.Close()

	broker := router.Events("env-1")
	require.NotNil(t, broker)
	assert.Same(t, broker, router.Events("env-1"))
	assert.Empty(t, factory.createdEnvs())
}

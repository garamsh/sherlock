package gateway_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/garamsh/sherlock/internal/agent"
	"github.com/garamsh/sherlock/internal/gateway"
)

func TestBroker_EmitWithZeroSubscribers(t *testing.T) {
	b := gateway.NewBroker()
	defer b.Close()
	// No subscribers is the normal case and must not block or panic.
	b.Emit(context.Background(), agent.Event{Kind: agent.EventTurnStarted, Turn: 1})
}

func TestBroker_SubscriberReceivesEvents(t *testing.T) {
	b := gateway.NewBroker()
	defer b.Close()
	ch, cancel := b.Subscribe()
	defer cancel()

	b.Emit(context.Background(), agent.Event{Kind: agent.EventTurnStarted, Turn: 1, Task: "hello"})

	select {
	case e := <-ch:
		assert.Equal(t, agent.EventTurnStarted, e.Kind)
		assert.Equal(t, "hello", e.Task)
	default:
		t.Fatal("expected event on subscriber channel")
	}
}

func TestBroker_CancelStopsDelivery(t *testing.T) {
	b := gateway.NewBroker()
	defer b.Close()
	ch, cancel := b.Subscribe()
	cancel()

	b.Emit(context.Background(), agent.Event{Kind: agent.EventTurnFinished, Turn: 1})

	// The channel is closed on cancel; no further events arrive.
	_, open := <-ch
	assert.False(t, open)
}

func TestBroker_SlowSubscriberDropsEvents(t *testing.T) {
	b := gateway.NewBroker()
	defer b.Close()
	ch, cancel := b.Subscribe()
	defer cancel()

	for i := range gateway.SubscriberBuffer * 2 {
		b.Emit(context.Background(), agent.Event{Kind: agent.EventTurnFinished, Turn: i})
	}

	assert.LessOrEqual(t, len(ch), gateway.SubscriberBuffer)
}

func TestBroker_CloseEndsStreams(t *testing.T) {
	b := gateway.NewBroker()
	ch, cancel := b.Subscribe()
	defer cancel()

	b.Close()
	_, open := <-ch
	assert.False(t, open)

	// Subscribing after close yields an already-closed channel.
	late, cancelLate := b.Subscribe()
	defer cancelLate()
	_, open = <-late
	assert.False(t, open)
}

func TestBroker_EmitAfterCloseIsDiscarded(t *testing.T) {
	b := gateway.NewBroker()
	b.Close()
	require.NotPanics(t, func() {
		b.Emit(context.Background(), agent.Event{Kind: agent.EventTurnStarted})
	})
}

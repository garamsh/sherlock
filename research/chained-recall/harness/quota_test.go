package main

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUsageLimit_RecognizesCode2056Only(t *testing.T) {
	assert.True(t, usageLimit(429, []byte(`{"base_resp":{"status_code":2056,"status_msg":"usage limit exceeded"}}`)))
	assert.True(t, usageLimit(429, []byte(`{"error":{"message":"Token Plan usage limit reached (2056)"}}`)))
	assert.False(t, usageLimit(429, []byte(`{"base_resp":{"status_code":1002,"status_msg":"rate limit exceeded(RPM)"}}`)), "the control: a rate limit is not the usage limit")
	assert.False(t, usageLimit(200, []byte(`2056`)))
}

func TestQuotaGate_PausesUntilAProbeSucceeds(t *testing.T) {
	var probes atomic.Int32
	g := &quotaGate{wait: 10 * time.Millisecond, probe: func(context.Context) (int, []byte) {
		if probes.Add(1) < 3 {
			return http.StatusTooManyRequests, []byte(`(2056)`)
		}
		return http.StatusOK, nil
	}}
	assert.False(t, g.paused(), "the control: an unclosed gate lets requests through")
	g.close()
	assert.True(t, g.paused())
	assert.Eventually(t, func() bool { return !g.paused() }, 2*time.Second, 5*time.Millisecond)
	assert.EqualValues(t, 3, probes.Load(), "two refused probes, then the one that reopens")
}

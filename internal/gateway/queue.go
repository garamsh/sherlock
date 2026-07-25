package gateway

// envQueue serializes one environment's runs: the inbox holds accepted
// followups in FIFO order and exactly one runner goroutine drains it, so
// there is never more than one active run per environment (ADR 0009).
type envQueue struct {
	envID   string
	runtime *Runtime
	inbox   chan Inbound
}

// run is the environment's runner loop. Each queued message is one turn
// of the rolling session; a failed turn is logged and the queue continues
// with the next followup.
func (r *Router) run(q *envQueue) {
	defer r.wg.Done()
	logger := r.logger.With("env_id", q.envID)
	for {
		select {
		case <-r.ctx.Done():
			return
		case msg := <-q.inbox:
			if _, err := q.runtime.Session.Run(r.ctx, msg.Content); err != nil {
				logger.Error("run failed",
					"op", "run", "message_id", msg.ID, "error", err)
				continue
			}
			logger.Info("run finished", "op", "run", "message_id", msg.ID)
		}
	}
}

package download

import (
	"context"
	"time"
)

// DefaultWorkerInterval is how often a running worker looks for new queue
// entries. Short enough that a POST is acted on promptly, long enough that an
// idle server does not keep querying the database for nothing.
const DefaultWorkerInterval = 30 * time.Second

// Worker drains the download queue on an interval, so a queue row added by
// the HTTP API or by `source pull` is acted on without someone running
// `download resume`.
//
// It is a convenience, not a lock. Two workers may run at once — a server and
// a manual resume, say — and the conditional claim in StartDownload decides
// each row between them. Nothing here needs to coordinate with anything else.
type Worker struct {
	service  *Service
	run      QueueRunConfig
	interval time.Duration
	// onCycle is called after each drain, which is how a caller reports
	// progress and survives a service that keeps erroring.
	onCycle func(DrainResult)
}

// DrainResult summarises one pass over the queue.
type DrainResult struct {
	Success int
	Failed  int
	// Err is non-nil when the pass itself failed. A per-track failure is
	// counted in Failed, not here: the pass still ran.
	Err error
}

// WorkerConfig configures a Worker. A zero Interval means
// DefaultWorkerInterval.
type WorkerConfig struct {
	Run      QueueRunConfig
	Interval time.Duration
	OnCycle  func(DrainResult)
}

// NewWorker wires a worker to a service.
func NewWorker(service *Service, cfg WorkerConfig) *Worker {
	interval := cfg.Interval
	if interval <= 0 {
		interval = DefaultWorkerInterval
	}

	return &Worker{service: service, run: cfg.Run, interval: interval, onCycle: cfg.OnCycle}
}

// Run drains the queue until ctx is cancelled.
//
// The first drain is immediate rather than one interval in, so a restart
// picks up work that was pending when the process went down.
func (w *Worker) Run(ctx context.Context, reporter Reporter) {
	if reporter == nil {
		reporter = defaultNoOpReporter
	}

	for {
		result, err := w.service.ProcessQueue(ctx, w.run, reporter)
		if w.onCycle != nil {
			w.onCycle(DrainResult{Success: result.Success, Failed: result.Failed, Err: err})
		}

		// A cancelled context is a shutdown, not a failure to report.
		if ctx.Err() != nil {
			return
		}

		timer := time.NewTimer(w.interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

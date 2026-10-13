package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/transactx/backend/internal/config"
	"github.com/transactx/backend/internal/recovery"
)

// Worker runs the platform background processing loops (recovery, delivery, etc.).
// It exposes NO public HTTP listener.
type Worker struct {
	env     *Environment
	config  config.Config
	logger  *slog.Logger
	stopCh  chan struct{}
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running atomic.Bool
}

// NewWorker constructs a Worker bound to the shared environment.
func NewWorker(env *Environment) *Worker {
	logger := env.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		env:    env,
		config: env.Config,
		logger: logger.With("service", "transactx-worker", "worker_label", env.Config.WorkerLabel),
		stopCh: make(chan struct{}),
	}
}

// Scheduler returns the recovery scheduler underlying this worker.
func (w *Worker) Scheduler() *recovery.Scheduler {
	return w.env.RecoveryScheduler
}

// IsRunning reports whether the worker is currently running.
func (w *Worker) IsRunning() bool {
	return w.running.Load()
}

// Run starts the independent bounded loops for background processing and blocks until
// context cancellation or Stop() is called.
func (w *Worker) Run(ctx context.Context) error {
	if !w.running.CompareAndSwap(false, true) {
		return errors.New("worker is already running")
	}
	defer w.running.Store(false)

	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	defer cancel()

	hasJobs := false

	scheduler := w.env.RecoveryScheduler
	if w.config.RecoveryEnabled && scheduler != nil {
		hasJobs = true
		// 1. Initial Heartbeat
		if err := scheduler.Heartbeat(runCtx); err != nil {
			w.logger.Warn("initial worker heartbeat failed", "error", err)
		} else {
			w.logger.Info("worker registered initial heartbeat",
				"worker_label", w.config.WorkerLabel,
				"families", w.config.WorkerJobFamilies,
				"build_sha", w.config.BuildSHA,
			)
		}

		// 2. Heartbeat Loop
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			ticker := time.NewTicker(w.config.RecoveryHeartbeat)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					if err := scheduler.Heartbeat(runCtx); err != nil && !errors.Is(err, context.Canceled) {
						w.logger.Warn("worker heartbeat failed", "error", err)
					}
				case <-runCtx.Done():
					return
				case <-w.stopCh:
					return
				}
			}
		}()

		// 3. Discovery Loop
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			ticker := time.NewTicker(w.config.RecoveryScanInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					discovered, err := scheduler.DiscoverStale(runCtx, 100)
					if err != nil && !errors.Is(err, context.Canceled) {
						w.logger.Warn("discovery scan failed", "error", err)
					} else if len(discovered) > 0 {
						w.logger.Info("discovered stale payments", "count", len(discovered))
					}
				case <-runCtx.Done():
					return
				case <-w.stopCh:
					return
				}
			}
		}()

		// 4. Sweep Interrupted Loop
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			sweepInterval := w.config.RecoveryLease / 2
			if sweepInterval < time.Second {
				sweepInterval = 5 * time.Second
			}
			ticker := time.NewTicker(sweepInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					swept, err := scheduler.SweepInterrupted(runCtx, 100)
					if err != nil && !errors.Is(err, context.Canceled) {
						w.logger.Warn("sweep interrupted attempts failed", "error", err)
					} else if swept > 0 {
						w.logger.Info("swept expired interrupted attempts", "count", swept)
					}
				case <-runCtx.Done():
					return
				case <-w.stopCh:
					return
				}
			}
		}()

		// 5. Claim and Process Loop (bounded polling)
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			for {
				select {
				case <-runCtx.Done():
					return
				case <-w.stopCh:
					return
				default:
					claimed, err := scheduler.ClaimAndProcessBatch(runCtx)
					if err != nil && !errors.Is(err, context.Canceled) {
						w.logger.Warn("claim and process batch failed", "error", err)
						select {
						case <-time.After(500 * time.Millisecond):
						case <-runCtx.Done():
							return
						case <-w.stopCh:
							return
						}
					} else if claimed == 0 {
						select {
						case <-time.After(200 * time.Millisecond):
						case <-runCtx.Done():
							return
						case <-w.stopCh:
							return
						}
					}
				}
			}
		}()
	}

	// 6. Analytics Window Aggregator Loop
	if w.env.AnalyticsService != nil {
		hasJobs = true
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			if processed, err := w.env.AnalyticsService.AggregateCompletedWindows(runCtx); err != nil && !errors.Is(err, context.Canceled) {
				w.logger.Warn("initial analytics window aggregation failed", "error", err)
			} else if processed > 0 {
				w.logger.Info("initial operational aggregate windows processed", "count", processed)
			}

			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					processed, err := w.env.AnalyticsService.AggregateCompletedWindows(runCtx)
					if err != nil && !errors.Is(err, context.Canceled) {
						w.logger.Warn("analytics window aggregation failed", "error", err)
					} else if processed > 0 {
						w.logger.Info("aggregated completed operational windows", "count", processed)
					}
				case <-runCtx.Done():
					return
				case <-w.stopCh:
					return
				}
			}
		}()
	}

	// 7. Investigator Claim and Execution Loop
	if w.env.InvestigatorService != nil && w.env.InvestigatorStore != nil && w.env.DB != nil {
		hasJobs = true
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					var jobID string
					err := w.env.DB.QueryRow(runCtx, `
						SELECT id FROM investigation_jobs
						WHERE status = 'queued' OR (status = 'running' AND lease_expires_at < NOW())
						ORDER BY created_at ASC
						LIMIT 1 FOR UPDATE SKIP LOCKED
					`).Scan(&jobID)
					if err == nil {
						w.logger.Info("worker executing investigator job", "job_id", jobID)
						if _, err := w.env.InvestigatorService.Run(runCtx, jobID, w.config.WorkerLabel); err != nil && !errors.Is(err, context.Canceled) {
							w.logger.Warn("worker run investigator job failed", "job_id", jobID, "error", err)
						}
					} else if !errors.Is(err, pgx.ErrNoRows) && !errors.Is(err, context.Canceled) {
						w.logger.Warn("investigator find candidate job failed", "error", err)
					}
				case <-runCtx.Done():
					return
				case <-w.stopCh:
					return
				}
			}
		}()
	}

	if !hasJobs {
		w.logger.Info("no background jobs configured; worker idling")
	}

	// Wait until context canceled or stop requested
	select {
	case <-runCtx.Done():
	case <-w.stopCh:
	}

	w.logger.Info("worker stopping; waiting for in-flight tasks to complete")
	w.wg.Wait()

	// Final heartbeat on shutdown
	if w.config.RecoveryEnabled && scheduler != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		_ = scheduler.Heartbeat(shutdownCtx)
	}
	w.logger.Info("worker stopped cleanly")
	return nil
}

// Stop signals the worker to stop claiming new jobs and wait for active jobs to finish.
func (w *Worker) Stop() {
	if w.running.Load() {
		select {
		case <-w.stopCh:
		default:
			close(w.stopCh)
		}
		if w.cancel != nil {
			w.cancel()
		}
	}
}

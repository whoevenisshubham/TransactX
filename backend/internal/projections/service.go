package projections

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/transactx/backend/internal/outbox"
)

// Service coordinates claiming, projection execution, and acknowledgement.
type Service struct {
	pool         *pgxpool.Pool
	store        outbox.Store
	dispatcher   *outbox.Dispatcher
	projector    *Projector
	consumerName string
}

func NewService(pool *pgxpool.Pool, store outbox.Store, dispatcher *outbox.Dispatcher, projector *Projector) *Service {
	return &Service{
		pool:         pool,
		store:        store,
		dispatcher:   dispatcher,
		projector:    projector,
		consumerName: outbox.ConsumerOpsWorklist,
	}
}

// ProcessBatch claims and processes up to maxCount due deliveries.
func (s *Service) ProcessBatch(ctx context.Context, workerID string, maxCount int) (processed int, err error) {
	deliveries, err := s.dispatcher.ClaimDueDeliveries(ctx, s.consumerName, workerID, maxCount, 30*time.Second)
	if err != nil {
		return 0, fmt.Errorf("claim deliveries: %w", err)
	}

	for _, d := range deliveries {
		event, err := s.store.GetEvent(ctx, d.EventID)
		if err != nil || event == nil {
			_, _ = s.dispatcher.QuarantineDelivery(ctx, d.ConsumerName, d.EventID, workerID, d.LeaseEpoch, "EVENT_NOT_FOUND")
			continue
		}

		duplicate, projErr := s.projector.ProjectEvent(ctx, event)
		if projErr != nil {
			if errors.Is(projErr, ErrUnsupportedVersion) {
				_, _ = s.dispatcher.QuarantineDelivery(ctx, d.ConsumerName, d.EventID, workerID, d.LeaseEpoch, "UNSUPPORTED_VERSION")
			} else {
				// Exponential backoff with jitter: min 100ms, max 1 minute
				backoffMs := (1 << d.Attempts) * 100
				if backoffMs > 60000 {
					backoffMs = 60000
				}
				jitter := rand.Intn(50)
				nextAttempt := time.Now().UTC().Add(time.Duration(backoffMs+jitter) * time.Millisecond)

				_, _ = s.dispatcher.FailDelivery(ctx, d.ConsumerName, d.EventID, workerID, d.LeaseEpoch, d.Attempts, projErr.Error(), nextAttempt)
			}
			continue
		}

		// Acknowledge delivery
		ok, compErr := s.dispatcher.CompleteDelivery(ctx, d.ConsumerName, d.EventID, workerID, d.LeaseEpoch)
		if compErr != nil {
			return processed, fmt.Errorf("complete delivery %s: %w", d.EventID, compErr)
		}
		if ok || duplicate {
			processed++
		}
	}

	return processed, nil
}

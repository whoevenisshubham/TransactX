package health

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidTarget = errors.New("health target is invalid")

type Service struct {
	repository *Repository
	config     Config
}

func NewService(repository *Repository, config Config) *Service {
	if !config.valid() {
		panic("invalid health configuration")
	}
	return &Service{repository: repository, config: config}
}

func (service *Service) RecordSample(ctx context.Context, sample HealthSample) error {
	if sample.TargetID == "" || sample.SampledAt.IsZero() || sample.Latency < 0 || (sample.Outcome != OutcomeSuccess && sample.Outcome != OutcomeFailure && sample.Outcome != OutcomeTimeout) {
		return ErrInvalidTarget
	}
	return service.repository.Record(ctx, sample)
}

func (service *Service) ListRecentSamples(ctx context.Context, targetID string, now time.Time) ([]HealthSample, error) {
	if targetID == "" {
		return nil, ErrInvalidTarget
	}
	return service.repository.ListRecent(ctx, targetID, now.Add(-service.config.Window), now, service.config.MaxSamples)
}

func (service *Service) GetSnapshot(ctx context.Context, targetID string, now time.Time) (HealthSnapshot, error) {
	samples, err := service.ListRecentSamples(ctx, targetID, now)
	if err != nil {
		return HealthSnapshot{}, err
	}
	return Snapshot(targetID, samples, now, service.config), nil
}

func (service *Service) Config() Config { return service.config }

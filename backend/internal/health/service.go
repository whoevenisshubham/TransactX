package health

import (
	"context"
	"errors"
	"time"

	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/common"
)

var (
	ErrInvalidTarget = errors.New("health target is invalid")
	ErrProbeRejected = errors.New("health probe rejected: circuit open or probe limit exceeded")
)

type Service struct {
	repository repository
	config     Config
	observer   func(HealthSample)
	probeGate  ProbeGate
}

type repository interface {
	Record(context.Context, HealthSample) error
	ListRecent(context.Context, string, time.Time, time.Time, int) ([]HealthSample, error)
}

type HealthChecker interface {
	GetHealth(context.Context) (bank.HealthResult, error)
}

// ProbeGate manages admission and slot release of recovery health probes.
type ProbeGate interface {
	BeforeHealthSample(targetID string, now ...time.Time) (allowed bool, isProbe bool)
	ReleaseProbe(targetID string)
}

func NewService(repository repository, config Config) *Service {
	if !config.valid() {
		panic("invalid health configuration")
	}
	return &Service{repository: repository, config: config}
}

func (service *Service) SetSampleObserver(observer func(HealthSample)) {
	service.observer = observer
}

func (service *Service) SetProbeGate(probeGate ProbeGate) {
	service.probeGate = probeGate
}

func (service *Service) Sample(ctx context.Context, targetID string, checker HealthChecker, now ...time.Time) (HealthSample, error) {
	if targetID == "" || checker == nil {
		return HealthSample{}, ErrInvalidTarget
	}
	startedAt := time.Now()
	if len(now) > 0 && !now[0].IsZero() {
		startedAt = now[0]
	}

	var isProbe bool
	if service.probeGate != nil {
		allowed, probe := service.probeGate.BeforeHealthSample(targetID, startedAt)
		if !allowed {
			return HealthSample{}, ErrProbeRejected
		}
		isProbe = probe
	}

	var probeCompleted bool
	if isProbe {
		defer func() {
			if !probeCompleted {
				service.probeGate.ReleaseProbe(targetID)
			}
		}()
	}

	checkContext, cancel := context.WithTimeout(ctx, service.config.TimeoutThreshold)
	result, err := checker.GetHealth(checkContext)
	cancel()

	latency := time.Since(startedAt)
	if latency < 0 {
		latency = 0
	}

	sample := HealthSample{
		TargetID:      targetID,
		SampledAt:     startedAt,
		Latency:       latency,
		CorrelationID: common.RequestIDFromContext(ctx),
	}
	if errors.Is(checkContext.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		sample.Outcome = OutcomeTimeout
	} else if err != nil || !result.Available {
		sample.Outcome = OutcomeFailure
	} else {
		sample.Available = true
		sample.Outcome = OutcomeSuccess
	}

	probeCompleted = true
	if isProbe && service.observer == nil {
		service.probeGate.ReleaseProbe(targetID)
	}

	if err := service.RecordSample(context.WithoutCancel(ctx), sample); err != nil {
		return HealthSample{}, err
	}
	return sample, nil
}

func (service *Service) RecordSample(ctx context.Context, sample HealthSample) error {
	if sample.TargetID == "" || sample.SampledAt.IsZero() || sample.Latency < 0 || (sample.Outcome != OutcomeSuccess && sample.Outcome != OutcomeFailure && sample.Outcome != OutcomeTimeout) {
		return ErrInvalidTarget
	}
	if service.observer != nil {
		service.observer(sample)
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

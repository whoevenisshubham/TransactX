package circuit

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidConfig = errors.New("invalid circuit breaker configuration")
)

type SuccessPolicy string

const (
	SuccessPolicyDecrement SuccessPolicy = "DECREMENT"
	SuccessPolicyReset     SuccessPolicy = "RESET"
)

type Config struct {
	FailureThreshold    int           `json:"failureThreshold"`
	TimeoutThreshold    int           `json:"timeoutThreshold"`
	RollingWindow       time.Duration `json:"rollingWindow"`
	OpenCooldown        time.Duration `json:"openCooldown"`
	HalfOpenProbeLimit  int           `json:"halfOpenProbeLimit"`
	SuccessThreshold    int           `json:"successThreshold"`
	RestorationSteps    int           `json:"restorationSteps"`
	SuccessPolicy       SuccessPolicy `json:"successPolicy"`
	StepSuccessThreshold int          `json:"stepSuccessThreshold"`
}

func DefaultConfig() Config {
	return Config{
		FailureThreshold:     5,
		TimeoutThreshold:     0, // 0 means timeouts count as failures toward FailureThreshold
		RollingWindow:        60 * time.Second,
		OpenCooldown:         30 * time.Second,
		HalfOpenProbeLimit:   2,
		SuccessThreshold:     2,
		RestorationSteps:     3,
		SuccessPolicy:        SuccessPolicyDecrement,
		StepSuccessThreshold: 2,
	}
}

func (c Config) Validate() error {
	if c.FailureThreshold <= 0 {
		return fmt.Errorf("%w: failureThreshold must be > 0, got %d", ErrInvalidConfig, c.FailureThreshold)
	}
	if c.TimeoutThreshold < 0 {
		return fmt.Errorf("%w: timeoutThreshold must be >= 0, got %d", ErrInvalidConfig, c.TimeoutThreshold)
	}
	if c.RollingWindow <= 0 {
		return fmt.Errorf("%w: rollingWindow must be positive, got %v", ErrInvalidConfig, c.RollingWindow)
	}
	if c.OpenCooldown <= 0 {
		return fmt.Errorf("%w: openCooldown must be positive, got %v", ErrInvalidConfig, c.OpenCooldown)
	}
	if c.HalfOpenProbeLimit <= 0 {
		return fmt.Errorf("%w: halfOpenProbeLimit must be > 0, got %d", ErrInvalidConfig, c.HalfOpenProbeLimit)
	}
	if c.SuccessThreshold <= 0 {
		return fmt.Errorf("%w: successThreshold must be > 0, got %d", ErrInvalidConfig, c.SuccessThreshold)
	}
	if c.RestorationSteps < 1 {
		return fmt.Errorf("%w: restorationSteps must be >= 1, got %d", ErrInvalidConfig, c.RestorationSteps)
	}
	if c.StepSuccessThreshold < 1 {
		return fmt.Errorf("%w: stepSuccessThreshold must be >= 1, got %d", ErrInvalidConfig, c.StepSuccessThreshold)
	}
	if c.SuccessPolicy != "" && c.SuccessPolicy != SuccessPolicyDecrement && c.SuccessPolicy != SuccessPolicyReset {
		return fmt.Errorf("%w: unknown successPolicy %q", ErrInvalidConfig, c.SuccessPolicy)
	}
	return nil
}

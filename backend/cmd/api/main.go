package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/chaos"
	"github.com/transactx/backend/internal/circuit"
	"github.com/transactx/backend/internal/config"
	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/health"
	apihttp "github.com/transactx/backend/internal/http"
	"github.com/transactx/backend/internal/payments"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("load configuration", "error", err)
		os.Exit(1)
	}

	db, err := database.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		logger.Error("connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	jwtManager, err := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTLifetime)
	if err != nil {
		logger.Error("configure JWT", "error", err)
		os.Exit(1)
	}
	authService := auth.NewService(db, jwtManager, cfg.DefaultBankCode)
	healthService := health.NewService(health.NewRepository(db), health.DefaultConfig())
	chaosRepo := chaos.NewRepository(db)
	chaosController := chaos.NewController(chaosRepo)

	adapters := make(map[uuid.UUID]bank.BankAdapter)
	healthTargets := make(map[string]health.HealthChecker)
	routeTargets := make(map[uuid.UUID]string)
	bankIDs := make(map[string]uuid.UUID)
	configureBank := func(url, code string) {
		if url == "" {
			return
		}
		var bankID uuid.UUID
		if err := db.QueryRow(context.Background(), `SELECT id FROM banks WHERE code = $1 AND status = 'ACTIVE'`, code).Scan(&bankID); err != nil {
			logger.Error("resolve configured bank", "code", code, "error", err)
			os.Exit(1)
		}
		adapter, clientErr := bank.NewHTTPClient(url, nil)
		if clientErr != nil {
			logger.Error("configure bank adapter", "code", code, "error", clientErr)
			os.Exit(1)
		}
		chaosAdapter := chaos.NewChaosAdapter(code, adapter, chaosController)
		adapters[bankID] = chaosAdapter
		healthTargets[code] = chaosAdapter
		routeTargets[bankID] = code
		bankIDs[code] = bankID
	}
	configureBank(os.Getenv("BANK_A_URL"), getEnv("BANK_A_CODE", "BANK-A"))
	configureBank(os.Getenv("BANK_B_URL"), getEnv("BANK_B_CODE", "BANK-B"))

	executionTargets := make(map[payments.RouteKey][]payments.ExecutionTarget)
	for _, targetCfg := range cfg.ExecutionTargets {
		srcID, srcOK := bankIDs[targetCfg.SourceBank]
		dstID, dstOK := bankIDs[targetCfg.DestinationBank]
		if !srcOK || !dstOK {
			logger.Error("execution target bank not found", "source_bank", targetCfg.SourceBank, "destination_bank", targetCfg.DestinationBank)
			os.Exit(1)
		}
		srcAdapter := adapters[srcID]
		if targetCfg.SourceEndpoint != "" {
			var err error
			srcAdapter, err = bank.NewHTTPClient(targetCfg.SourceEndpoint, nil)
			if err != nil {
				logger.Error("configure source adapter for execution target", "candidate_id", targetCfg.CandidateID, "error", err)
				os.Exit(1)
			}
		}
		dstAdapter := adapters[dstID]
		if targetCfg.DestinationEndpoint != "" {
			var err error
			dstAdapter, err = bank.NewHTTPClient(targetCfg.DestinationEndpoint, nil)
			if err != nil {
				logger.Error("configure destination adapter for execution target", "candidate_id", targetCfg.CandidateID, "error", err)
				os.Exit(1)
			}
		}
		if targetCfg.Endpoint != "" {
			endpointAdapter, err := bank.NewHTTPClient(targetCfg.Endpoint, nil)
			if err != nil {
				logger.Error("configure health target for execution target", "target_id", targetCfg.ExecutionTargetID, "error", err)
				os.Exit(1)
			}
			healthTargets[targetCfg.ExecutionTargetID] = chaos.NewChaosAdapter(targetCfg.ExecutionTargetID, endpointAdapter, chaosController)
		}
		key := payments.RouteKey{SourceBankID: srcID, DestinationBankID: dstID}
		executionTargets[key] = append(executionTargets[key], payments.ExecutionTarget{
			CandidateID:        targetCfg.CandidateID,
			ExecutionTargetID:  targetCfg.ExecutionTargetID,
			SourceAdapter:      chaos.NewChaosAdapter(targetCfg.ExecutionTargetID, srcAdapter, chaosController),
			DestinationAdapter: chaos.NewChaosAdapter(targetCfg.ExecutionTargetID, dstAdapter, chaosController),
		})
	}

	circuitCfg := circuit.Config{
		FailureThreshold:     cfg.CircuitFailureThreshold,
		TimeoutThreshold:     cfg.CircuitTimeoutThreshold,
		RollingWindow:        cfg.CircuitRollingWindow,
		OpenCooldown:         cfg.CircuitOpenCooldown,
		HalfOpenProbeLimit:   cfg.CircuitHalfOpenProbeLimit,
		SuccessThreshold:     cfg.CircuitSuccessThreshold,
		RestorationSteps:     cfg.CircuitRestorationSteps,
		SuccessPolicy:        circuit.SuccessPolicy(cfg.CircuitSuccessPolicy),
		StepSuccessThreshold: 2,
	}
	circuitRepo := circuit.NewRepository(db)
	circuitBreaker, err := circuit.NewBreaker(circuitCfg, circuitRepo)
	if err != nil {
		logger.Error("initialize circuit breaker", "error", err)
		os.Exit(1)
	}
	healthService.SetSampleObserver(circuitBreaker.RecordHealthSample)
	healthService.SetProbeGate(circuitBreaker)

	var executionTargetIDs []string
	for _, targets := range executionTargets {
		for _, et := range targets {
			if et.ExecutionTargetID != "" {
				executionTargetIDs = append(executionTargetIDs, et.ExecutionTargetID)
			}
		}
	}
	var bankCodes []string
	for code := range bankIDs {
		bankCodes = append(bankCodes, code)
	}
	var explicitHealthTargetIDs []string
	for targetID := range healthTargets {
		if _, isBank := bankIDs[targetID]; !isBank {
			explicitHealthTargetIDs = append(explicitHealthTargetIDs, targetID)
		}
	}
	chaosController.SetTargetValidator(chaos.BuildTargetValidator(explicitHealthTargetIDs, executionTargetIDs, bankCodes...))
	if err := chaosController.Hydrate(context.Background()); err != nil {
		logger.Error("hydrate chaos scenarios", "error", err)
		os.Exit(1)
	}

	var handler http.Handler
	if len(adapters) == 0 {
		handler = apihttp.NewHandlerWithChaos(db, logger, authService, jwtManager, chaosController)
	} else if len(executionTargets) > 0 {
		handler = apihttp.NewHandlerWithExecutionTargetsCircuitAndChaos(db, logger, authService, jwtManager, adapters, healthTargets, executionTargets, healthService, payments.SelectionMode(cfg.RoutingMode), cfg.RoutingStaticBaseline, circuitBreaker, chaosController)
	} else {
		handler = apihttp.NewHandlerWithBankAdaptersHealthRoutingAndChaos(db, logger, authService, jwtManager, adapters, healthTargets, routeTargets, healthService, chaosController)
	}
	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("API listening", "address", cfg.Address)
		serverErrors <- server.ListenAndServe()
	}()

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			logger.Error("API server stopped", "error", err)
			os.Exit(1)
		}
	case <-shutdownContext.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			logger.Error("shutdown API server", "error", err)
			os.Exit(1)
		}
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

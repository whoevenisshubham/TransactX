package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/transactx/backend/internal/accounts"
	"github.com/transactx/backend/internal/analytics"
	"github.com/transactx/backend/internal/audit"
	"github.com/transactx/backend/internal/auth"
	"github.com/transactx/backend/internal/bank"
	"github.com/transactx/backend/internal/chaos"
	"github.com/transactx/backend/internal/circuit"
	"github.com/transactx/backend/internal/config"
	"github.com/transactx/backend/internal/database"
	"github.com/transactx/backend/internal/health"
	apihttp "github.com/transactx/backend/internal/http"
	"github.com/transactx/backend/internal/investigator"
	"github.com/transactx/backend/internal/operations"
	"github.com/transactx/backend/internal/outbox"
	"github.com/transactx/backend/internal/payments"
	"github.com/transactx/backend/internal/projections"
	"github.com/transactx/backend/internal/recipients"
	"github.com/transactx/backend/internal/reconciliation"
	"github.com/transactx/backend/internal/recovery"
	"github.com/transactx/backend/internal/telemetry"
)

// Environment encapsulates the initialized platform runtime components shared between API and Worker.
type Environment struct {
	Config               config.Config
	DB                   *pgxpool.Pool
	Logger               *slog.Logger
	AuthService          *auth.Service
	JWTManager           *auth.JWTManager
	HealthService        *health.Service
	HealthTargets        map[string]health.HealthChecker
	ChaosRepo            chaos.Repository
	ChaosController      *chaos.Controller
	CircuitBreaker       *circuit.CircuitBreaker
	Adapters             map[uuid.UUID]bank.BankAdapter
	RouteTargets         map[uuid.UUID]string
	BankIDs              map[string]uuid.UUID
	ExecutionTargets     map[payments.RouteKey][]payments.ExecutionTarget
	ReconciliationEngine *reconciliation.Engine
	ParticipantSources   map[string]reconciliation.LedgerSnapshotSource
	PaymentsService      *payments.Service
	RecoveryRepo         *recovery.Repository
	RecoveryScheduler    *recovery.Scheduler
	TelemetryProvider    *telemetry.Provider
	TelemetryShutdown    func(context.Context) error
	AnalyticsRepo        analytics.Repository
	AnalyticsService     *analytics.Service
	InvestigatorStore    investigator.InvestigationStore
	CorpusRepo           *investigator.PostgresCorpusRepository
	InvestigatorService  *investigator.Service
	ProjectionsService   *projections.Service
	ownsDB               bool
}

// BuildEnvironment establishes the database pool and constructs the shared platform environment.
func BuildEnvironment(ctx context.Context, cfg config.Config, logger *slog.Logger) (*Environment, error) {
	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required to build environment")
	}
	db, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	env, err := BuildEnvironmentWithPool(ctx, cfg, db, logger)
	if err != nil {
		db.Close()
		return nil, err
	}
	env.ownsDB = true
	return env, nil
}

// BuildEnvironmentWithPool builds the shared platform environment using an existing database connection pool.
func BuildEnvironmentWithPool(ctx context.Context, cfg config.Config, db *pgxpool.Pool, logger *slog.Logger) (*Environment, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if db == nil {
		return nil, errors.New("database pool is required")
	}

	jwtManager, err := auth.NewJWTManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTLifetime)
	if err != nil {
		return nil, fmt.Errorf("configure JWT: %w", err)
	}
	authService := auth.NewService(db, jwtManager, cfg.DefaultBankCode)
	if cfg.ParticipantProvisioning {
		authService.WithParticipantProvisioner(auth.NewSimulationParticipantAccountProvisioner())
	}
	healthService := health.NewService(health.NewRepository(db), health.DefaultConfig())
	chaosRepo := chaos.NewRepository(db)
	chaosController := chaos.NewController(chaosRepo)

	adapters := make(map[uuid.UUID]bank.BankAdapter)
	healthTargets := make(map[string]health.HealthChecker)
	routeTargets := make(map[uuid.UUID]string)
	bankIDs := make(map[string]uuid.UUID)

	configureBank := func(bankURL, code, token string) error {
		if bankURL == "" {
			return nil
		}
		var bankID uuid.UUID
		if err := db.QueryRow(ctx, `SELECT id FROM banks WHERE code = $1 AND status = 'ACTIVE'`, code).Scan(&bankID); err != nil {
			return fmt.Errorf("resolve configured bank %s: %w", code, err)
		}
		adapter, clientErr := bank.NewHTTPClient(bankURL, nil)
		if clientErr != nil {
			return fmt.Errorf("configure bank adapter %s: %w", code, clientErr)
		}
		if token != "" {
			adapter.SetServiceToken(token)
		}
		chaosAdapter := chaos.NewChaosAdapter(code, adapter, chaosController)
		adapters[bankID] = chaosAdapter
		healthTargets[code] = chaosAdapter
		routeTargets[bankID] = code
		bankIDs[code] = bankID
		return nil
	}

	if err := configureBank(cfg.BankAURL, cfg.BankACode, cfg.BankAServiceToken); err != nil {
		return nil, err
	}
	if err := configureBank(cfg.BankBURL, cfg.BankBCode, cfg.BankBServiceToken); err != nil {
		return nil, err
	}

	executionTargets := make(map[payments.RouteKey][]payments.ExecutionTarget)
	for _, targetCfg := range cfg.ExecutionTargets {
		srcID, srcOK := bankIDs[targetCfg.SourceBank]
		dstID, dstOK := bankIDs[targetCfg.DestinationBank]
		if !srcOK || !dstOK {
			return nil, fmt.Errorf("execution target bank not found: %s -> %s", targetCfg.SourceBank, targetCfg.DestinationBank)
		}
		srcAdapter := adapters[srcID]
		if targetCfg.SourceEndpoint != "" {
			var err error
			srcAdapter, err = bank.NewHTTPClient(targetCfg.SourceEndpoint, nil)
			if err != nil {
				return nil, fmt.Errorf("configure source adapter for %s: %w", targetCfg.CandidateID, err)
			}
		}
		dstAdapter := adapters[dstID]
		if targetCfg.DestinationEndpoint != "" {
			var err error
			dstAdapter, err = bank.NewHTTPClient(targetCfg.DestinationEndpoint, nil)
			if err != nil {
				return nil, fmt.Errorf("configure destination adapter for %s: %w", targetCfg.CandidateID, err)
			}
		}
		if targetCfg.Endpoint != "" {
			endpointAdapter, err := bank.NewHTTPClient(targetCfg.Endpoint, nil)
			if err != nil {
				return nil, fmt.Errorf("configure health target for %s: %w", targetCfg.ExecutionTargetID, err)
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
		return nil, fmt.Errorf("initialize circuit breaker: %w", err)
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
	if err := chaosController.Hydrate(ctx); err != nil {
		return nil, fmt.Errorf("hydrate chaos scenarios: %w", err)
	}

	knownParticipants := make(reconciliation.KnownParticipants)
	participantAdapters := make(map[string]bank.BankAdapter)
	for code, bankID := range bankIDs {
		knownParticipants[code] = true
		participantAdapters[code] = adapters[bankID]
	}
	reconRepo := reconciliation.NewRunRepository(db)
	reconEngine := reconciliation.NewEngine(
		knownParticipants,
		reconRepo,
		func(_ context.Context, participantID string, _ reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			return reconciliation.NewDurableCentralRepositoryParticipant(db, participantID, 15*time.Minute)
		},
		func(_ context.Context, participantID string, _ reconciliation.Scope) (reconciliation.ReconciliationParticipant, error) {
			adapter, ok := participantAdapters[participantID]
			if !ok {
				return nil, reconciliation.ErrInvalidParticipant
			}
			return reconciliation.NewDurableRepositoryParticipant(
				db, adapter, participantID, "participant:"+participantID, 15*time.Minute,
			)
		},
	)
	participantSources := make(map[string]reconciliation.LedgerSnapshotSource, len(participantAdapters))
	for code, adapter := range participantAdapters {
		participantSources[code] = adapter
	}

	accountsRepo := accounts.NewRepository(db)
	recipientsRepo := recipients.NewRepository(db)
	paymentsRepo := payments.NewRepository(db)
	var paymentsSvc *payments.Service
	if len(adapters) == 0 {
		paymentsSvc = payments.NewService(accountsRepo, recipientsRepo, paymentsRepo, nil)
	} else if len(executionTargets) > 0 {
		paymentsSvc = payments.NewServiceWithExecutionTargets(accountsRepo, recipientsRepo, paymentsRepo, adapters, executionTargets, healthService, payments.SelectionMode(cfg.RoutingMode), cfg.RoutingStaticBaseline)
	} else if len(routeTargets) > 0 {
		paymentsSvc = payments.NewServiceWithAdaptiveRouting(accountsRepo, recipientsRepo, paymentsRepo, adapters, routeTargets, healthService)
	} else {
		paymentsSvc = payments.NewServiceWithAdapters(accountsRepo, recipientsRepo, paymentsRepo, adapters)
	}
	paymentsSvc.SetCircuitEligibility(circuitBreaker.EligibilityHook())
	paymentsSvc.SetPaymentConcurrency(cfg.PaymentConcurrency)

	recoveryRepo := recovery.NewRepository(db)
	var recoveryScheduler *recovery.Scheduler
	if cfg.RecoveryEnabled {
		recoveryCfg := recovery.Config{
			WorkerLabel:    cfg.WorkerLabel,
			BuildSHA:       cfg.BuildSHA,
			Concurrency:    cfg.RecoveryConcurrency,
			StaleAge:       cfg.RecoveryStaleAfter,
			ScanInterval:   cfg.RecoveryScanInterval,
			LeaseDuration:  cfg.RecoveryLease,
			AttemptTimeout: cfg.RecoveryAttemptTimeout,
			MaxAttempts:    cfg.RecoveryMaxAttempts,
			JobFamilies:    cfg.WorkerJobFamilies,
		}
		var err error
		recoveryScheduler, err = recovery.NewScheduler(recoveryRepo, paymentsSvc, recoveryCfg)
		if err != nil {
			return nil, fmt.Errorf("initialize recovery scheduler: %w", err)
		}
	}

	telCfg := telemetry.DefaultConfig("transactx-api")
	telCfg.Environment = "development"
	if !cfg.DevelopmentMode {
		telCfg.Environment = "production"
	}
	if cfg.BuildSHA != "" {
		telCfg.BuildSHA = cfg.BuildSHA
	}
	telProvider, telShutdown, err := telemetry.InitProvider(ctx, telCfg, db, logger)
	if err != nil {
		logger.Warn("initialize telemetry provider failed, proceeding without otel", "error", err)
	}

	analyticsRepo := analytics.NewPostgresRepository(db)
	var entityTargets []analytics.EntityTarget
	for code := range bankIDs {
		entityTargets = append(entityTargets, analytics.EntityTarget{Type: "bank", ID: code})
	}
	for _, targets := range executionTargets {
		for _, t := range targets {
			entityTargets = append(entityTargets, analytics.EntityTarget{Type: "execution_target", ID: t.ExecutionTargetID})
		}
	}
	analyticsSvc := analytics.NewService(analyticsRepo, entityTargets, logger)

	operationsRepo := operations.NewRepository(db)
	operationsSvc := operations.NewService(operationsRepo, adapters, logger)

	corpusRepo, err := investigator.NewPostgresCorpusRepository(db)
	if err != nil {
		return nil, fmt.Errorf("initialize corpus repository: %w", err)
	}
	investigatorStore, err := investigator.NewPostgresInvestigationStore(db, time.Now)
	if err != nil {
		return nil, fmt.Errorf("initialize investigator store: %w", err)
	}

	bridgedEvidence := investigator.BridgedEvidenceReader{
		Source: &opsEvidenceSource{ops: operationsSvc},
	}
	investigatorDeps := investigator.ToolDependencies{
		Evidence:  bridgedEvidence,
		Runbooks:  corpusRepo,
		Recovery:  &opsRecoveryReader{ops: operationsSvc},
		Trace:     &opsTraceReader{traceStore: telemetry.NewTraceStore(db, logger)},
		Analytics: &opsAnalyticsReader{repo: analyticsRepo},
	}
	invCfg := investigator.DefaultServiceConfig()
	investigatorSvc, err := investigator.NewService(invCfg, investigatorStore, investigatorDeps)
	if err != nil {
		return nil, fmt.Errorf("initialize investigator service: %w", err)
	}

	outboxStore := outbox.NewPostgresStore(db)
	outboxDispatcher := outbox.NewDispatcher(db)
	projector := projections.NewProjector(db, outbox.ConsumerOpsWorklist)
	projectionsSvc := projections.NewService(db, outboxStore, outboxDispatcher, projector)

	manifestPath := "docs/runbooks/corpus-manifest.json"
	rootDir := "."
	if _, err := os.Stat(manifestPath); err != nil {
		if _, err2 := os.Stat(filepath.Join("..", manifestPath)); err2 == nil {
			rootDir = ".."
			manifestPath = filepath.Join("..", manifestPath)
		}
	}
	if build, err := investigator.BuildCorpus(rootDir, manifestPath); err == nil {
		if err := corpusRepo.Stage(ctx, build); err == nil {
			_ = corpusRepo.Activate(ctx, build.Version, build.ManifestSHA256)
		}
	}

	return &Environment{
		Config:               cfg,
		DB:                   db,
		Logger:               logger,
		AuthService:          authService,
		JWTManager:           jwtManager,
		HealthService:        healthService,
		HealthTargets:        healthTargets,
		ChaosRepo:            chaosRepo,
		ChaosController:      chaosController,
		CircuitBreaker:       circuitBreaker,
		Adapters:             adapters,
		RouteTargets:         routeTargets,
		BankIDs:              bankIDs,
		ExecutionTargets:     executionTargets,
		ReconciliationEngine: reconEngine,
		ParticipantSources:   participantSources,
		PaymentsService:      paymentsSvc,
		RecoveryRepo:         recoveryRepo,
		RecoveryScheduler:    recoveryScheduler,
		TelemetryProvider:    telProvider,
		TelemetryShutdown:    telShutdown,
		AnalyticsRepo:        analyticsRepo,
		AnalyticsService:     analyticsSvc,
		InvestigatorStore:    investigatorStore,
		CorpusRepo:           corpusRepo,
		InvestigatorService:  investigatorSvc,
		ProjectionsService:   projectionsSvc,
	}, nil
}

// Close releases the resources owned by Environment.
func (e *Environment) Close() {
	if e.TelemetryShutdown != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = e.TelemetryShutdown(shutdownCtx)
		cancel()
	}
	if e.ownsDB && e.DB != nil {
		e.DB.Close()
	}
}

// NewHTTPHandler constructs the API HTTP handler backed by the Environment.
func (e *Environment) NewHTTPHandler() http.Handler {
	runtimeOptions := apihttp.RuntimeOptions{
		SimulationMode:         e.Config.SimulationMode,
		CORSAllowedOrigins:     e.Config.CORSAllowedOrigins,
		ParticipantSources:     e.ParticipantSources,
		AllowRegistration:      e.Config.AllowRegistration,
		ChaosEnabled:           e.Config.ChaosEnabled,
		RecoveryControlEnabled: e.Config.RecoveryControlEnabled,
		AuditStore:             audit.NewPostgresStore(e.DB),
	}
	var handler http.Handler
	if len(e.Adapters) == 0 {
		handler = apihttp.NewHandlerWithReconciliationAndRuntimeOptions(e.DB, e.Logger, e.AuthService, e.JWTManager, e.ChaosController, e.ReconciliationEngine, runtimeOptions)
	} else if len(e.ExecutionTargets) > 0 {
		handler = apihttp.NewHandlerWithExecutionTargetsCircuitChaosReconciliationAndRuntimeOptions(e.DB, e.Logger, e.AuthService, e.JWTManager, e.Adapters, e.HealthTargets, e.ExecutionTargets, e.HealthService, payments.SelectionMode(e.Config.RoutingMode), e.Config.RoutingStaticBaseline, e.CircuitBreaker, e.ChaosController, e.ReconciliationEngine, runtimeOptions)
	} else {
		handler = apihttp.NewHandlerWithBankAdaptersHealthRoutingChaosReconciliationAndRuntimeOptions(e.DB, e.Logger, e.AuthService, e.JWTManager, e.Adapters, e.HealthTargets, e.RouteTargets, e.HealthService, e.ChaosController, e.ReconciliationEngine, runtimeOptions)
	}
	if e.TelemetryProvider != nil {
		handler = telemetry.HTTPMiddleware(e.TelemetryProvider.Tracer(), e.TelemetryProvider.Metrics())(handler)
	}
	return handler
}

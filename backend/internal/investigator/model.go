// Package investigator defines read-only integration seams. No runtime orchestration is implemented in C0.
package investigator

type AnalyticsWindow struct {
	Id                   string        `json:"id"`
	EntityType           string        `json:"entityType"`
	EntityId             string        `json:"entityId"`
	From                 string        `json:"from"`
	To                   string        `json:"to"`
	SampleCount          int64         `json:"sampleCount"`
	FeatureSchemaVersion string        `json:"featureSchemaVersion"`
	Features             FeatureVector `json:"features"`
	BaselineAlert        *Warning      `json:"baselineAlert"`
	Model                struct {
		Version   *string  `json:"version"`
		Status    string   `json:"status"`
		Score     *float64 `json:"score"`
		Threshold *float64 `json:"threshold"`
	} `json:"model"`
	Advisory bool `json:"advisory"`
}

type Assertion struct {
	Field      string  `json:"field"`
	Value      string  `json:"value"`
	EntityId   *string `json:"entityId"`
	EvidenceId string  `json:"evidenceId"`
}

type CentralPaymentState string

type EvidenceBundle struct {
	SchemaVersion        string          `json:"schemaVersion"`
	PaymentId            string          `json:"paymentId"`
	SnapshotId           string          `json:"snapshotId"`
	CollectedAt          string          `json:"collectedAt"`
	Payment              PaymentSnapshot `json:"payment"`
	Route                Route           `json:"route"`
	Operations           []Operation     `json:"operations"`
	Transitions          []Transition    `json:"transitions"`
	TransitionsTruncated bool            `json:"transitionsTruncated"`
	Recovery             RecoverySummary `json:"recovery"`
	Reconciliation       struct {
		SourceStatus     SourceStatus `json:"sourceStatus"`
		RunIds           []string     `json:"runIds"`
		DiscrepancyCount *int64       `json:"discrepancyCount"`
		ScopeMismatch    bool         `json:"scopeMismatch"`
	} `json:"reconciliation"`
	Integrity struct {
		SourceStatus   SourceStatus `json:"sourceStatus"`
		RunId          *string      `json:"runId"`
		OccurredAt     *string      `json:"occurredAt"`
		ViolationCount *int64       `json:"violationCount"`
	} `json:"integrity"`
	Traces struct {
		SourceStatus SourceStatus `json:"sourceStatus"`
		TraceIds     []string     `json:"traceIds"`
		Truncated    bool         `json:"truncated"`
	} `json:"traces"`
	Sources  []Source  `json:"sources"`
	Warnings []Warning `json:"warnings"`
}

type Fact struct {
	Text          string      `json:"text"`
	EvidenceIds   []string    `json:"evidenceIds"`
	Assertions    []Assertion `json:"assertions"`
	Contradiction bool        `json:"contradiction"`
}

type FeatureVector struct {
	TimeoutRate                *float64 `json:"timeoutRate"`
	FailureRate                *float64 `json:"failureRate"`
	P95LatencyMs               *float64 `json:"p95LatencyMs"`
	OldestRecoveryAgeMs        *float64 `json:"oldestRecoveryAgeMs"`
	RecoveryAttemptsPerPayment *float64 `json:"recoveryAttemptsPerPayment"`
	HealthAvailabilityRate     *float64 `json:"healthAvailabilityRate"`
}

type Inference struct {
	Text          string   `json:"text"`
	EvidenceIds   []string `json:"evidenceIds"`
	Contradiction bool     `json:"contradiction"`
}

type InvestigationReport struct {
	SchemaVersion    string           `json:"schemaVersion"`
	Mode             string           `json:"mode"`
	Summary          string           `json:"summary"`
	Facts            []Fact           `json:"facts"`
	Inferences       []Inference      `json:"inferences"`
	Recommendations  []Recommendation `json:"recommendations"`
	MissingEvidence  []string         `json:"missingEvidence"`
	EvidenceCoverage string           `json:"evidenceCoverage"`
	SnapshotId       string           `json:"snapshotId"`
	CollectedAt      string           `json:"collectedAt"`
}

type Operation struct {
	OperationId       string       `json:"operationId"`
	OperationType     string       `json:"operationType"`
	BankCode          string       `json:"bankCode"`
	CentralStatus     string       `json:"centralStatus"`
	ParticipantStatus *string      `json:"participantStatus"`
	Presence          string       `json:"presence"`
	LastObservedAt    *string      `json:"lastObservedAt"`
	SourceStatus      SourceStatus `json:"sourceStatus"`
	EvidenceId        string       `json:"evidenceId"`
}

type PaymentSnapshot struct {
	State        CentralPaymentState `json:"state"`
	AmountPaise  string              `json:"amountPaise"`
	Currency     string              `json:"currency"`
	CreatedAt    string              `json:"createdAt"`
	UpdatedAt    string              `json:"updatedAt"`
	CompletedAt  *string             `json:"completedAt"`
	EventVersion string              `json:"eventVersion"`
	EvidenceId   string              `json:"evidenceId"`
}

type Recommendation struct {
	Text            string   `json:"text"`
	RunbookChunkIds []string `json:"runbookChunkIds"`
}

type RecoveryAttempt struct {
	AttemptId    string   `json:"attemptId"`
	PaymentId    string   `json:"paymentId"`
	WorkerId     string   `json:"workerId"`
	StartedAt    string   `json:"startedAt"`
	FinishedAt   *string  `json:"finishedAt"`
	StateBefore  string   `json:"stateBefore"`
	StateAfter   *string  `json:"stateAfter"`
	Result       string   `json:"result"`
	ReasonCode   *string  `json:"reasonCode"`
	OperationIds []string `json:"operationIds"`
	TraceId      *string  `json:"traceId"`
	LeaseEpoch   string   `json:"leaseEpoch"`
}

type RecoverySummary struct {
	Status        string       `json:"status"`
	AttemptCount  int64        `json:"attemptCount"`
	NextAttemptAt *string      `json:"nextAttemptAt"`
	LastAttemptId *string      `json:"lastAttemptId"`
	ReasonCode    *string      `json:"reasonCode"`
	SourceStatus  SourceStatus `json:"sourceStatus"`
}

type Route struct {
	SourceBankCode      *string      `json:"sourceBankCode"`
	DestinationBankCode *string      `json:"destinationBankCode"`
	ExecutionTargetId   *string      `json:"executionTargetId"`
	SelectedAt          *string      `json:"selectedAt"`
	ReasonCode          *string      `json:"reasonCode"`
	Status              SourceStatus `json:"status"`
}

type Source struct {
	Name       string       `json:"name"`
	Status     SourceStatus `json:"status"`
	ObservedAt *string      `json:"observedAt"`
	ErrorCode  *string      `json:"errorCode"`
}

type SourceStatus string

type Transition struct {
	Id         string `json:"id"`
	Sequence   string `json:"sequence"`
	FromState  string `json:"fromState"`
	ToState    string `json:"toState"`
	OccurredAt string `json:"occurredAt"`
	EvidenceId string `json:"evidenceId"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

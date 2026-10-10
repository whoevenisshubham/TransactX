# TransactX autonomous implementation pack

Version 1.0. Prepared 11 October 2026. Planning deliverable only; this pack does not implement the proposed features.

TransactX will become a distributed payment reliability and operations platform: deterministic simulated payments, crash recovery, reliable operational events, evidence-led investigations, useful anomaly detection, and a polished online demonstration. Its financial kernel remains authoritative. The intelligent layer explains evidence and never moves money.

## Give each agent the right entry document

1. Keep this folder intact. Markdown is the authoritative format because agents can search and read individual sections without loading a large Word document.
2. Give M1 this pack and `prompts/M1_START.md`.
3. Give M2 this pack and `prompts/M2_START.md`.
4. Give M3 this pack and `prompts/M3_START.md`.
5. Each agent starts from the current canonical GitHub branch in its own checkout. They can start together. Interface fixtures allow independent progress; integration follows dependency gates.
6. M1 installs the shared planning files into the repository and coordinates release integration. M2 and M3 must not install competing copies or edit the shared plan independently.

Repository: https://github.com/whoevenisshubham/TransactX

Verified canonical branch: `m1/foundation`.

Verified planning baseline: `2b9f6e6702d2db54651d71859f29d6cbc3eb90fc`.

Verify the remote default again at implementation start. If it has advanced, inspect the difference from this SHA and preserve valid newer work. Never reset the repository to this historical SHA merely to match the plan.

## What the finished release contains

- Automatic, bounded recovery of interrupted routed payments, with crash and concurrent-worker evidence.
- A PostgreSQL transactional outbox, duplicate-safe worklist projection, retries, quarantine, and rebuild tools.
- An operator worklist and a complete payment evidence view with route, operation, recovery, reconciliation, and trace context.
- OpenTelemetry instrumentation, useful metrics, JSON logs, and a bounded trace explorer.
- A read-only investigator with approved-document retrieval, typed evidence tools, verified citations, optional real LLM generation, and evaluation tests.
- Operational analytics, a statistical baseline, and a small, evaluated anomaly model that never changes routing or settlement.
- A redacted evidence export, backup and restore rehearsal, secure demonstration roles, and deployment automation.
- React on Vercel or another static host; native Go processes and PostgreSQL on one free-eligible Linux VM. No local Docker prerequisite.

Live paid LLM calls are disabled by default. With no provider key or usable local model, the release shows a real evidence summary and retrieval results, clearly labelled as evidence-only. Provider integration can be implemented and tested with fixtures; live LLM validation remains explicitly outstanding until a real model is configured. Do not call a fixture a live AI result.

## Read this order

All agents: `AGENT_RULES.md`, `docs/next-gen/SOURCE_BASELINE.md`, `docs/next-gen/API_CONTRACTS.md`, and their own manual. Then read only the technical documents named by the next task.

M1 manual: `manuals/M1_PRODUCT_AND_DELIVERY.md`.

M2 manual: `manuals/M2_RELIABILITY_AND_PLATFORM.md`.

M3 manual: `manuals/M3_INVESTIGATION_AND_ANALYTICS.md`.

Human overview: `docs/next-gen/TRANSACTX_V2_MASTER_PLAN.md`.

Integration order: `docs/next-gen/EXECUTION_AND_GIT.md` and `docs/next-gen/INTERVIEW_10_DAY_SPRINT.md`.

## Model settings

Use the lightest model that passes the task's tests. Recommended starting settings for the models available in this app:

| Work | Starting model | Reasoning |
| --- | --- | --- |
| Small UI changes, runbooks, fixtures, release packaging | GPT-6 Luna | Medium; low for mechanical edits |
| Financial recovery, outbox, authentication, AI orchestration, model scoring | GPT-6.1 Sol | Medium |
| Final review of concurrency, crash safety, security or conflicting evidence | GPT-6.1 Sol | High, for a bounded review |

These are recommendations, not a claim that every account has these choices or the same usage accounting. If unavailable, use the available Sol or Luna equivalent for the same role. A Work subscription and an application's LLM API budget are separate. Do not purchase credits, enable paid cloud resources, or switch everything to the largest model.

The token-saving rules and escalation policy are in `docs/next-gen/EXECUTION_AND_GIT.md`. Small task context, durable checkpoints, and focused verification matter more than asking a model to use fewer tokens.

## One-time access that agents cannot invent

The owners must supply repository access, permission to open and merge their branches, a connected free hosting account, and any necessary authentication. Public deployment also needs a valid HTTPS backend hostname or a verified public-IP certificate path. An agent cannot promise a free VM will be available, bypass provider identity checks, or operate through an exhausted account allowance.

Once those prerequisites exist, the start prompts authorize implementation, commits, pushes, PRs, gated integration, and deployment of the simulation within free limits. Missing external access blocks that external action only; agents continue independent local work and leave a runnable release bundle.

No document can guarantee zero defects, completion in a few days, or indefinite free uptime. This pack makes completion measurable and provides continuation instructions so failures do not turn into invented success.

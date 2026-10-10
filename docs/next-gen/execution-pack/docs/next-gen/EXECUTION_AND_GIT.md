# Autonomous execution and Git integration

## Before starting

Each partner gives their own start prompt to an agent that has this pack, local execution tools and repository access. These documents do not themselves create or schedule background agents. A model can work only while its task/environment and account allowance permit. When interrupted or allowance is exhausted, checkpoint and resume from that checkpoint; do not restart the audit.

All three lanes can start independently. A fixture permits coding before a backend dependency exists; it does not satisfy the eventual integration gate. Read `TASK_REGISTRY.json` for implementation dependencies and integration requirements. No lane can merge a contract-breaking change without updating affected consumers through M2/M1.

## Model and token policy

Recommended coding choices use the currently documented efficient Luna role and everyday coding Sol role. Exact model availability and subscription usage vary; API prices do not establish Work quota consumption.

| Task type | Recommendation | Escalation |
| --- | --- | --- |
| Fixtures, runbooks, UI extraction, CSS, deployment packaging | GPT-6 Luna medium; low for purely mechanical edits | Sol medium if a concrete unresolved bug needs deeper diagnosis |
| Recovery, SQL concurrency, outbox, auth, investigator orchestration, ML scoring | GPT-6.1 Sol medium | Sol high for a bounded correctness review or hard reproduced failure |
| Financial/security final review | GPT-6.1 Sol high | Larger model only after a documented failure demonstrates need and the owner permits usage |

Do not use maximum reasoning, Pro mode, many nested agents or repeated full-history copies by default. One agent per owner is enough. The project-level model used by its coding agent is separate from the application's runtime model.

Concrete savings:

- Read the short shared rules, baseline, contract and own task card once; then read only named technical sections/source files. Target a compact initial task context around 6-8K tokens when possible, with further reads justified by correctness.
- Use `rg` for symbols/paths, bounded file sections and batches of independent reads. Avoid repeatedly dumping lockfiles, whole repositories or prior handoffs.
- Keep one coherent session per lane. Store progress in lane state, not repeated long explanations. After compaction resume the next recorded task.
- Run focused tests first; inspect failures and exit codes. Full suites run at lane integration/release gates, and again only if new changes justify them.
- Reuse existing helpers and proven core code. Avoid stack migrations, formatting churn, duplicate abstractions and tutorial research.
- Save large logs to files; read concise failure extracts. Never hide a failed exit code because the output was redirected.
- Use one brief progress note per meaningful milestone and a compact final result; do not generate duplicate reports for each commit.
- After two failed attempts on the same reproduced issue, stop blind edits, write a short failure hypothesis, inspect the missing source boundary and escalate one reasoning level for that issue. Do not purchase credits.
- If credits end, write a checkpoint with branch/SHA/dirty paths/test failures/next action and stop safely. Resume work when the account permits; elapsed time is not approval to lower quality.

## Git topology

Preserve default `m1/foundation` and historical RC1 state. M1 creates `release/transactx-v2` from the current verified canonical branch and installs shared docs/contracts. Owner branches:

- `v2/m1-product-delivery`
- `v2/m2-reliability-platform`
- `v2/m3-investigation-analytics`

Each owner uses a separate checkout/worktree. Never let three agents modify the same working directory. Existing uncommitted changes are preserved; use an isolated checkout if their ownership is uncertain.

M2/M3 may begin from canonical HEAD before M1's bootstrap arrives, reading the attached pack locally. They later merge the integration branch into their owner branch. They do not commit independent copies of shared planning files.

Default branch verification example: `git ls-remote --symref origin HEAD`; then fetch the verified branch. Do not hard-code `main` or rename default branches.

## Ownership and shared files

M2 exclusively edits `backend/internal/payments`, `auth`, `bank`, `bankservice`, shared `http/handler.go`, `cmd/api`, `cmd/worker`, configuration, `backend/go.mod/go.sum`, migrations and shared API contracts. M3 may propose a patch for mounting its package/dependencies but does not independently edit those files while M2 owns them.

M1 exclusively edits frontend and its dependency files, deployment, browser tests, release packaging, integration state and public documentation. M3 exclusively edits investigator/anomaly packages, analytics tooling, corpus, ML/evaluation artifacts and its state. Existing reconciliation changes require M2 ownership and an invariant-backed reason.

No single shared progress file edited by everyone. Each lane writes `docs/next-gen/state/M1.md`, M2.md or M3.md. M1 writes the release summary after integration. Task registry is planning metadata; execution evidence is in state, PR descriptions and release reports.

## Commit behavior

Create a commit after a coherent reviewable change passes its focused checks. A complex task may naturally contain implementation, failure-test and documentation commits; a small task may need one. Commit count is not a target. Never split trivial lines into fake activity, backdate work, invent coauthors or remove required attribution to appear human.

Good messages describe the behavior:

- `Use one recovery path for request retries and workers`
- `Resolve interrupted bank calls with the original operation identity`
- `Keep worklist updates safe when events are delivered twice`
- `Show missing bank evidence in payment details`
- `Add a restore rehearsal for the demo database`

Include a short body when it explains a non-obvious safety decision and the tests. Use the owner's legitimately configured Git identity. Check `git diff --check`, staged paths and secrets before committing. Do not commit build output/node_modules/dumps/token files.

## Pull requests and integration

Open draft PRs against `release/transactx-v2`. Group related slices into a coherent PR; roughly three or four milestone PRs per large lane is sufficient if the diffs stay reviewable. Describe problem/result/validation/limitations. Use a body file or structured argument to preserve actual newlines. Attach created PRs to the task when the host offers that capability.

M1 is the sole integrator. Before each merge, verify exact PR head SHA, required CI, compatibility fixtures and task acceptance evidence. Perform a focused financial/security review with Sol high where necessary. Do not merge stale checks after an additional push. Respect branch protection and ownership; missing rights leave a PR ready and explicitly blocked on access.

Merge compatible milestones in this sequence:

1. Shared planning/contracts and baseline/migration tool.
2. M2 serialization/presence/security foundations.
3. M2 scheduler/worker/outbox/projector and M1 fixture-built worklist/evidence UI.
4. M2 evidence/telemetry/analytics plus M3 retrieval/investigator packages.
5. M2 mounting and M3 scoring/evaluation plus M1 integrated AI/analytics UI.
6. Combined tests, deployment/restore, final evidence.

For owner branch updates use normal merges from the integration branch, not force-pushed rebases after others depend on the branch. Conflict resolution preserves the shared contract and valid upstream changes. Never choose one side wholesale to get a clean merge. Run touched checks after resolving.

After each integration, owners fetch once at their next task boundary and merge if needed. Do not poll every minute or reread all PR logs. Git/PR state is the cross-account coordination channel; other chats are not magically shared memory.

## Task completion record

For each task record: ID/status, branch/head SHA, changed paths, acceptance checks and actual result, environment/skip limitations, PR link, source artifact link, next ready task and blocker if any. Status is planned/running/implemented/verified/integrated/blocked. `implemented` is not `verified`.

When a dependency is absent, finish fixture-supported work and the next independent task. Leave the live-integration gate pending. Do not wait indefinitely, create fake substitute behavior in production or duplicate another lane's package.

## Authorized autonomy and hard boundaries

The provided start prompts authorize changes within this plan, legitimate commits/pushes/PRs, and M1's gated integration/deployment into configured free resources. Routine engineering choices do not require another question. Agents must not spend money, modify real financial data, publish credentials, delete unrelated files, bypass account checks or override branch protections.

External account login/verification, absent Git permissions, required deployment credentials and paid services cannot be solved by instructions alone. Report the exact missing prerequisite once, continue independent work, and deliver runnable artifacts. Do not call the project fully deployed when only the frontend exists.

## Deviation rule

The plan controls scope; actual code controls implementation facts. A necessary bug fix inside scope is allowed with tests and a recorded rationale. A changed upstream API or safety assumption requires a compact deviation note with evidence, impact and compatible adaptation. Financial invariants and spending limits cannot be waived by the agent. Optional P2/P3 ideas never displace unfinished P0 work.

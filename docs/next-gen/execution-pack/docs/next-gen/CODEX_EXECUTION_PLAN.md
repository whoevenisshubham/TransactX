# Agent entry and continuation guide

Use START_HERE, AGENT_RULES, EXECUTION_AND_GIT and the owner manual. Do not paste every document into every request.

## Execution loop

1. Read own state and actual branch status. Verify current integration contract version.
2. Choose the next dependency-ready task in TASK_REGISTRY.
3. Read its card and only the named technical/source sections.
4. Implement the bounded slice and meaningful boundary tests.
5. Run focused checks; diagnose and repair failures without weakening tests.
6. Review diff/security/invariants; make a coherent commit and push when access permits.
7. Update state with tested SHA/results/next action; open or update a reviewable PR.
8. Continue next ready task. Live dependency gates remain pending until real integration.

M1 additionally integrates under exact-head checks and release gates. All agents preserve valid upstream work and avoid force-pushed history.

## Checkpoint format

```text
Owner and task:
Status:
Branch and last committed SHA:
Uncommitted paths and purpose:
Contract/corpus/model versions:
Checks run and actual results:
Environment skips or external blockers:
PR/artifact links:
Next exact action and required files:
```

Keep a checkpoint under 500 words unless a real failure needs more detail. Never store secrets or copied entire logs. On resumption inspect dirty work and finish it before starting an unrelated task.

## Handling blockers

A code failure is a debugging task. A missing dependency permits fixture-supported implementation plus the next independent task. A missing account/credential/paid budget blocks only that external action. A changed financial assumption blocks unsafe execution until the invariant is repaired or a narrow human decision is obtained. Do not repeatedly ask for routine approval.

Do not automate model switching through guessed app APIs. The user selects a suggested available model; an agent may recommend a bounded escalation when needed. Do not start recurring tasks or other chats merely because this pack discusses several days of work.

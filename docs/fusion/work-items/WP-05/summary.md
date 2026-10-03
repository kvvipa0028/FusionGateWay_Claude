# WP-05

Completed a separate private local fusion.db, checksummed migration, immutable plan/route/evidence references, project-scoped idempotency, If-Match revisions, ordered events, one active task attempt, startup intent, owner/generation fencing and durable unknown recovery. No Magpie account database is opened.

15 store tests passed, one isolated helper skipped in the parent and executed by the native SIGKILL test; combined 47 Fusion top-level tests with subscenarios and race passed. Targeted vet passed. The crash test preserved a committed native session and synthetic file effect, with no new attempt on restart. Observed lease expiry is committed before returning fenced, so clock rollback cannot restore the old worker.

Task success/acceptance, authenticated APIs, real Runtime startup/sandbox/stop and backup/restore remain future work. Final test families stay not_run.

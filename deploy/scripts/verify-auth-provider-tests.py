"""Require actual Go test passes, never absence or a PostgreSQL skip."""
import json
import sys

required = {
    ("auth", "TestGoogleDurableFlowsAndProvisioning"),
    ("http", "TestGoogleHTTPBindingDenialAndOrigins"),
    ("investigator", "TestGeminiDurableAdmissionAndGrounding"),
    ("investigator", "TestGeminiAtomicVerificationAdmission"),
    ("investigator", "TestGeminiFailuresAndDisableRetainEvidence"),
    ("investigator", "TestPostgresInvestigationStoreUpgradedDatabaseConformance"),
    ("app", "TestGeminiColdCompositionWithOwnerReview"),
}
outcomes = {}
with open(sys.argv[1], encoding="utf-8-sig") as stream:
    for line in stream:
        event = json.loads(line)
        key = (event.get("Package", "").rsplit("/", 1)[-1], event.get("Test"))
        if key in required and event.get("Action") in {"pass", "skip", "fail"}:
            outcomes[key] = event["Action"]
missing = sorted(key for key in required if outcomes.get(key) != "pass")
if missing:
    sys.exit("Required integration acceptance did not pass: " + repr(missing))
print(f"All {len(required)} required PostgreSQL acceptance tests passed (none skipped).")

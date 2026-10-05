"""Offline historical human decision shape; this is not an authority importer."""
import copy
from datetime import datetime
import re
import json
from pathlib import Path

import jsonschema

packet = Path(__file__).resolve().parent
root = packet.parents[4]
schema = json.loads((root / "docs/fusion/contracts/human-decision.schema.json").read_text())
jsonschema.Draft202012Validator.check_schema(schema)
checker = jsonschema.FormatChecker()


@checker.checks("date-time", raises=(ValueError, TypeError))
def valid_date(value):
    # Do not rely on jsonschema's optional RFC3339 dependency being installed.
    if not isinstance(value, str) or not re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?Z", value):
        return False
    return datetime.fromisoformat(value.replace("Z", "+00:00")).tzinfo is not None


validator = jsonschema.Draft202012Validator(schema, format_checker=checker)
capture = json.loads((packet / "actual-human-capture.json").read_text())
decision = capture["decision"]
report = capture["report"]
validator.validate(decision)
assert decision["action"] == "accept" and capture["task"]["state"] == "completed"
assert report["hard"]["status"] == "passed"
assert report["model_valid"] and report["model"]["verdict"] == "accepted"
assert decision["task_id"] == capture["task"]["id"]
for key in ("run_id", "text_hash", "tree_hash", "spec_hash", "design_hash", "acceptance_hash"):
    assert decision[key] == report[key], key
for mode in ("model_action", "worker_authority", "missing_run", "bad_hash",
             "empty_reason", "version", "generation", "unknown_field", "bad_date"):
    value = copy.deepcopy(decision)
    if mode == "model_action":
        value["action"] = "accepted"
    elif mode == "worker_authority":
        value["authority"] = "model"
    elif mode == "missing_run":
        del value["run_id"]
    elif mode == "bad_hash":
        value["spec_hash"] = "self-declared"
    elif mode == "empty_reason":
        value["reason"] = ""
    elif mode == "version":
        value["version"] = 2
    elif mode == "generation":
        value["generation"] = 0
    elif mode == "unknown_field":
        value["bypass_hard_check"] = True
    else:
        value["at"] = "tomorrow"
    assert list(validator.iter_errors(value)), mode
result = {
    "historical_human_receipt_shape": "pass",
    "hard_model_and_explicit_action_separate": "pass",
    "exact_report_identity": "pass",
    "negative_documents": 9,
    "schema_does_not_mint_final_evidence_or_prove_human_ui_action": True,
}
(packet / "schema-results.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result))

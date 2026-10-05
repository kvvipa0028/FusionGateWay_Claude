"""Offline model document/hash checks; no Native origin or human authority."""
import copy
import hashlib
import json
from pathlib import Path

import jsonschema

packet = Path(__file__).resolve().parent
root = packet.parents[4]
schema = json.loads((root / "docs/fusion/contracts/acceptance.schema.json").read_text())
jsonschema.Draft202012Validator.check_schema(schema)
validator = jsonschema.Draft202012Validator(schema)
capture = json.loads((packet / "actual-acceptance-capture.json").read_text())
opinion = capture["artifact"]["acceptance"]
validator.validate(opinion["document"])
assert json.loads(opinion["text"]) == opinion["document"]
assert hashlib.sha256(opinion["text"].encode()).hexdigest() == opinion["text_hash"]
assert capture["hard_verdict"]["status"] == "passed"
assert capture["task"]["state"] == "advisory_only"
assert opinion["valid"] and opinion["criteria_count"] == 1
assert opinion["review_run_id"] == capture["artifact"]["parent_run_id"]
for mode in ("missing_criteria", "unknown_field", "null_index", "negative_index",
             "accepted_unverified", "rejected_all_met", "version", "uppercase"):
    doc = copy.deepcopy(opinion["document"])
    if mode == "missing_criteria":
        doc["criteria"] = []
    elif mode == "unknown_field":
        doc["human_accepted"] = True
    elif mode == "null_index":
        doc["criteria"][0]["index"] = None
    elif mode == "negative_index":
        doc["criteria"][0]["index"] = -1
    elif mode == "accepted_unverified":
        doc["criteria"][0]["status"] = "unverified"
    elif mode == "rejected_all_met":
        doc["verdict"] = "rejected"
    elif mode == "version":
        doc["version"] = 2
    else:
        doc["Version"] = doc.pop("version")
    assert list(validator.iter_errors(doc)), mode
result = {
    "captured_private_advisory_acceptance": "pass",
    "actual_text_hash": "pass",
    "hard_model_and_human_authority_separate": "pass",
    "negative_documents": 8,
    "exact_approved_coverage_and_duplicate_fields_enforced_by_go": True,
    "schema_does_not_establish_native_origin_or_human_acceptance": True,
}
(packet / "schema-results.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result))

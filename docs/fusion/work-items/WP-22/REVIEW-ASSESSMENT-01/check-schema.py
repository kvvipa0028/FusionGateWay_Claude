"""Offline schema and captured private-text integrity; no model authority."""
import copy
import hashlib
import json
from pathlib import Path

import jsonschema

packet = Path(__file__).resolve().parent
root = packet.parents[4]
schema = json.loads((root / "docs/fusion/contracts/review.schema.json").read_text())
jsonschema.Draft202012Validator.check_schema(schema)
validator = jsonschema.Draft202012Validator(schema)
capture = json.loads((packet / "actual-review-capture.json").read_text())
opinion = capture["artifact"]["review"]
validator.validate(opinion["document"])
assert json.loads(opinion["text"]) == opinion["document"]
assert hashlib.sha256(opinion["text"].encode()).hexdigest() == opinion["text_hash"]
assert capture["hard_verdict"]["status"] == "passed"
assert capture["workflow"]["next"] == "acceptance"
assert capture["workflow"]["approval"] is not None
for mode in ("missing_findings", "unknown_field", "accepted", "blocking_approve", "empty_changes", "version", "uppercase"):
    doc = copy.deepcopy(opinion["document"])
    if mode == "missing_findings":
        del doc["findings"]
    elif mode == "unknown_field":
        doc["human_accepted"] = True
    elif mode == "accepted":
        doc["verdict"] = "accepted"
    elif mode == "blocking_approve":
        doc["findings"] = [{"id": "R1", "severity": "blocking", "summary": "needs fix"}]
    elif mode == "empty_changes":
        doc["verdict"] = "changes_required"
    elif mode == "version":
        doc["version"] = 2
    else:
        doc["Version"] = doc.pop("version")
    assert list(validator.iter_errors(doc)), mode
result = {"captured_private_advisory_review": "pass", "actual_text_hash": "pass", "hard_model_and_approval_separate": "pass", "negative_documents": 7, "schema_does_not_establish_native_origin_or_acceptance": True}
(packet / "schema-results.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result))

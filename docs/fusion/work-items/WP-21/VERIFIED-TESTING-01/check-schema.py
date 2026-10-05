"""Offline captured owned record/header shape checks; no evidence import grant."""
import copy
import hashlib
import json
from pathlib import Path

import jsonschema

packet = Path(__file__).resolve().parent
root = packet.parents[4]
capture = json.loads((packet / "actual-capture.json").read_text())
validators = {}
for name in ("handoff", "evidence"):
    schema = json.loads((root / f"docs/fusion/contracts/{name}.schema.json").read_text())
    jsonschema.Draft202012Validator.check_schema(schema)
    validators[name] = jsonschema.Draft202012Validator(schema, format_checker=jsonschema.FormatChecker())
validators["handoff"].validate(capture["handoff"])
record = capture["verification"]["data"]["record"]
validators["evidence"].validate(record)
assert hashlib.sha256((packet / "actual-report.xml").read_bytes()).hexdigest() == record["report_hash"]
assert hashlib.sha256((packet / "actual-stderr.log").read_bytes()).hexdigest() == record["stderr_hash"]
legacy = json.loads((root / "docs/fusion/work-items/WP-20/FROZEN-ARTIFACT-01/serialized-fixture.json").read_text())
validators["handoff"].validate(legacy)
for mode in ("false_pass", "wrong_role", "unexecuted_pending", "missing_pending", "unknown_status", "text_boolean"):
    doc = copy.deepcopy(capture["handoff"])
    if mode == "false_pass":
        doc["evidence"]["tests_executed"] = False
        doc["pending"].append("engineering_tests_not_executed")
    elif mode == "wrong_role":
        doc["binding"]["role"] = "design"
    elif mode == "unexecuted_pending":
        doc["pending"].append("engineering_tests_not_executed")
    elif mode == "missing_pending":
        doc["pending"].remove("acceptance_pending")
    elif mode == "unknown_status":
        doc["evidence"]["status"] = "accepted"
    else:
        doc["evidence"]["tests_executed"] = "true"
    assert list(validators["handoff"].iter_errors(doc)), mode
result = {"actual_owned_header_and_record": "pass", "report_and_stderr_hashes": "pass", "legacy_advisory_header": "pass", "negative_headers": 6, "origin_or_next_stage_grant": "not_proved_by_schema"}
(packet / "schema-results.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result))

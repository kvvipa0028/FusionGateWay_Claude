#!/usr/bin/env python3
"""Offline structural check; digest identity/provenance is tested by Go APIs."""
import copy
import json
from pathlib import Path

import jsonschema

HERE = Path(__file__).resolve().parent
schema = json.loads((HERE / "../../../contracts/handoff.schema.json").read_text())
document = json.loads((HERE / "serialized-fixture.json").read_text())
jsonschema.Draft202012Validator.check_schema(schema)
validator = jsonschema.Draft202012Validator(schema)
validator.validate(document)
cases = {}
for name in ["version", "session", "tests", "success", "digest", "missing_run", "empty_change", "entry_path", "unknown_evidence"]:
    d = copy.deepcopy(document)
    if name == "version":
        d["version"] = 2
    elif name == "session":
        d["native_session_id"] = "fixture-untrusted"
    elif name == "tests":
        d["evidence"]["tests_executed"] = True
    elif name == "success":
        d["evidence"]["status"] = "passed"
    elif name == "digest":
        d["binding"]["plan_hash"] = "malformed"
    elif name == "missing_run":
        del d["binding"]["run_id"]
    elif name == "empty_change":
        d["changes"][0]["before"] = d["changes"][0]["after"] = None
    elif name == "entry_path":
        d["artifact"]["entries"][0]["path"] = "../escape"
    elif name == "unknown_evidence":
        d["evidence"]["admitted"] = True
    errors = list(validator.iter_errors(d))
    assert errors, f"negative fixture accepted: {name}"
    cases[name] = "rejected"
result = {"schema": "handoff v1", "serialized_producer_fixture": "passed", "negative_cases": cases, "provenance_and_supplier_admission": "not_proved_by_schema"}
(HERE / "schema-results.json").write_text(json.dumps(result, ensure_ascii=False, indent=2) + "\n")
print(json.dumps(result, ensure_ascii=False))

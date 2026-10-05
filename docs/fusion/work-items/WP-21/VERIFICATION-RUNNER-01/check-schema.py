"""Offline shape validation of a real captured record, not an import gate."""
import json
import hashlib
from pathlib import Path
import jsonschema

root = Path(__file__).resolve().parents[5]
packet = Path(__file__).resolve().parent
schema = json.loads((root / "docs/fusion/contracts/evidence.schema.json").read_text())
record = json.loads((packet / "actual-record.json").read_text())
jsonschema.Draft202012Validator.check_schema(schema)
validator = jsonschema.Draft202012Validator(schema, format_checker=jsonschema.FormatChecker())
validator.validate(record)
assert hashlib.sha256((packet / "actual-report.xml").read_bytes()).hexdigest() == record["report_hash"]
assert hashlib.sha256((packet / "actual-stderr.log").read_bytes()).hexdigest() == record["stderr_hash"]
for change in ({"exit_code": "0"}, {"executed": "true"},
               {"artifact_hash": "model says passed"}, {"tool_version": ""}, {"version": 2}):
    assert list(validator.iter_errors({**record, **change})), change
print("Actual record and five invalid types/identities: PASS")

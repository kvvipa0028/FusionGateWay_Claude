"""Validate actual Go and prior direct receipts; JSON never grants execution."""
import copy
import datetime
import json
import re
import sys
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker

root = Path(__file__).resolve().parents[5]
schema = json.loads((root / 'docs/fusion/contracts/evidence.schema.json').read_text())
Draft202012Validator.check_schema(schema)
formats = FormatChecker()

@formats.checks('date-time', raises=ValueError)
def utc_time(value):
    if not isinstance(value, str):
        return True
    if not re.fullmatch(r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?Z', value):
        return False
    datetime.datetime.strptime(value[:19], '%Y-%m-%dT%H:%M:%S')
    return True

validator = Draft202012Validator(schema, format_checker=formats)
actual = json.loads(Path(sys.argv[1]).read_text())
old = json.loads((root / 'docs/fusion/work-items/WP-21/VERIFICATION-RUNNER-01/actual-record.json').read_text())
checks = []
for name, record in [('actual-go', actual), ('prior-direct', old)]:
    validator.validate(record)
    checks.append({'name': name, 'expected': 'valid', 'passed': True})

def negative(name, record, mutate):
    changed = copy.deepcopy(record)
    mutate(changed)
    assert list(validator.iter_errors(changed)), name
    checks.append({'name': name, 'expected': 'invalid', 'passed': True})

negative('missing-go-commands', actual, lambda v: v.pop('go_commands'))
negative('foreign-toolchain-field', actual, lambda v: v['spec']['go_toolchain'].update(network=True))
negative('invalid-sdk-hash', actual, lambda v: v['spec']['go_toolchain'].update(root_hash='unknown'))
negative('missing-vendor-mode', actual, lambda v: v['spec']['go_toolchain'].pop('vendor'))
negative('incomplete-chain', actual, lambda v: v.update(go_commands=v['go_commands'][:1]))
negative('unbounded-chain', actual, lambda v: v['go_commands'].extend(v['go_commands']))
negative('foreign-command-grant', actual, lambda v: v['go_commands'][1].update(allow_shell=True))
negative('invalid-phase-image', actual, lambda v: v['go_commands'][3].update(image_hash='x'*64))
negative('invalid-phase-time', actual, lambda v: v['go_commands'][3].update(started_at='yesterday'))
negative('unbounded-phase-environment', actual, lambda v: v['go_commands'][1].update(environment=['x']*33))
negative('direct-imported-go-commands', old, lambda v: v.update(go_commands=actual['go_commands']))
negative('direct-expanded-environment', old, lambda v: v['environment'].append('GOPROXY=https://unexpected.invalid'))
print(json.dumps({'schema_valid': True, 'checks': checks}, indent=2))

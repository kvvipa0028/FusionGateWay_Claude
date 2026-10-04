#!/usr/bin/env python3
"""Offline checks of the implemented API contract and captured Handler JSON.

Requires PyYAML, jsonschema and referencing. The official OpenAPI document
schema is supplied explicitly; this checker never retrieves remote resources.
"""
import argparse
import copy
import json
import re
from pathlib import Path

import yaml
from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT202012

EXPECTED = {
    "/control/v1/projects/{project_id}/submission": {"get", "post"},
    "/control/v1/projects/{project_id}/submission/acknowledge": {"post"},
    "/control/v1/projects/{project_id}/submission/abandon": {"post"},
    "/control/v1/projects": {"get"},
    "/control/v1/projects/{project_id}/tasks": {"get"},
    "/control/v1/projects/{project_id}/tasks/before/{task_id}": {"get"},
    "/control/v1/tasks/{task_id}/cancel": {"post"},
    "/control/v1/tasks/{task_id}/pause": {"post"},
    "/control/v1/tasks/{task_id}/continue": {"post"},
    "/control/v1/defaults/global": {"get", "put"},
    "/control/v1/defaults/global/versions/{revision}": {"get"},
    "/control/v1/projects/{project_id}/defaults": {"get", "put"},
    "/control/v1/projects/{project_id}/defaults/versions/{revision}": {"get"},
    "/agent/v1/tasks": {"post"},
    "/agent/v1/tasks/{task_id}": {"get"},
    "/agent/v1/tasks/{task_id}/runs/{run_id}": {"get"},
    "/agent/v1/tasks/{task_id}/events": {"get"},
    "/control/v1/tasks/preview": {"post"},
    "/control/v1/tasks/{task_id}/budget": {"get"},
    "/control/v1/tasks/{task_id}/plan": {"get", "put"},
    "/control/v1/tasks/{task_id}/plan/preview": {"post"},
    "/control/v1/tasks/{task_id}/start": {"post"},
    "/control/v1/tasks/{task_id}/resume": {"post"},
    "/control/v1/tasks/{task_id}/runs/{run_id}/cancel": {"post"},
    "/control/v1/tasks/{task_id}/runs/{run_id}/checkpoint": {"post"},
    "/control/v1/tasks/{task_id}/preset": {"get"},
    "/control/v1/projects/{project_id}/configuration": {"get"},
    "/control/v1/projects/{project_id}/presets": {"get"},
    "/control/v1/projects/{project_id}/presets/{preset_id}": {"get", "put"},
    "/control/v1/projects/{project_id}/presets/{preset_id}/versions/{revision}": {"get"},
    "/control/v1/projects/{project_id}/quota": {"get"},
    "/control/v1/projects/{project_id}/quota/{route_id}/refresh": {"post"},
}


def verify(contract, official, samples):
    doc = yaml.safe_load(contract.read_text())
    standard = json.loads(official.read_text())
    assert standard["$id"] == "https://spec.openapis.org/oas/3.1/schema/2022-10-07"
    Draft202012Validator(standard).validate(doc)
    assert doc["openapi"] == "3.1.0"
    assert doc["x-fusion-production-registered"] is False
    methods = {"get", "post", "put", "delete", "patch", "head", "options", "trace"}
    actual = {path: set(item) & methods for path, item in doc["paths"].items()}
    assert actual == EXPECTED, "implemented Handler path/method coverage differs"
    operations = []
    for path, item in doc["paths"].items():
        for method in actual[path]:
            op = item[method]
            operations.append(op["operationId"])
            params = op.get("parameters", [])
            assert {p["name"] for p in params if p["in"] == "path"} == set(re.findall(r"\{([^}]+)\}", path))
            assert all(p["required"] for p in params if p["in"] == "path")
    assert len(set(operations)) == len(operations)

    root_uri = contract.resolve().as_uri()
    resources = [(root_uri, Resource.from_contents(doc, default_specification=DRAFT202012))]
    for name in ["stage-plan.schema.json", "quota-snapshot.schema.json"]:
        path = contract.parent / "contracts" / name
        content = json.loads(path.read_text())
        resource = Resource.from_contents(content)
        resources.append((path.resolve().as_uri(), resource))
        if "$id" in content:
            resources.append((content["$id"], resource))
    registry = Registry().with_resources(resources)
    checker = FormatChecker()

    def validator(schema):
        # Give relative and fragment refs the OpenAPI document's base URI.
        value = copy.deepcopy(schema)
        value["$id"] = root_uri
        value["components"] = doc["components"]
        return Draft202012Validator(value, registry=registry, format_checker=checker)

    def walk(value):
        if isinstance(value, dict):
            if "$ref" in value:
                registry.resolver(root_uri).lookup(value["$ref"])
            for child in value.values():
                walk(child)
        elif isinstance(value, list):
            for child in value:
                walk(child)
    walk(doc)
    for name, schema in doc["components"]["schemas"].items():
        Draft202012Validator.check_schema(schema)
        if schema.get("type") == "object":
            assert "additionalProperties" in schema, f"unbounded object schema {name}"
            assert "properties" in schema or isinstance(schema["additionalProperties"], dict) or schema.get("x-fusion-open-json"), f"placeholder object schema {name}"

    def response(op, status):
        result = op["responses"][str(status)]
        if "$ref" in result:
            result = registry.resolver(root_uri).lookup(result["$ref"]).contents
        return result

    covered = set()
    count = 0
    for sample in json.loads(samples.read_text()):
        matches = [path for path in EXPECTED if re.fullmatch(re.sub(r"\{[^}]+\}", r"[^/]+", path), sample["path"])]
        assert len(matches) == 1, "captured response has no unique documented path"
        path, method = matches[0], sample["method"].lower()
        if sample["status"] == 405 and method not in EXPECTED[path]:
            op = doc["paths"][path][sorted(EXPECTED[path])[0]]
        else:
            assert method in EXPECTED[path]
            op = doc["paths"][path][method]
        resp = response(op, sample["status"])
        schema = resp["content"][sample["media_type"]]["schema"]
        if sample["media_type"] == "text/event-stream":
            schema = op["x-fusion-event-data-schema"]
        validator(schema).validate(sample["body"])
        if 200 <= sample["status"] < 300:
            covered.add((path, method))
            if "requestBody" in op:
                validator(op["requestBody"]["content"]["application/json"]["schema"]).validate(sample["request"])
            for param in op.get("parameters", []):
                if param["in"] == "header" and param.get("required"):
                    validator(param["schema"]).validate(sample["request_headers"][param["name"]])
        for name, value in sample.get("headers", {}).items():
            assert name in resp.get("headers", {}), "response header missing from contract"
            validator(resp["headers"][name]["schema"]).validate(value)
        count += 1
    assert covered == {(path, method) for path, methods in EXPECTED.items() for method in methods}, "missing successful Handler operation samples"

    # Confirm strict DTO schemas reject privilege-bearing fields. These probes
    # guard the documentation's input boundary; runtime authorization has its
    # own tests and is not proven by a JSON schema.
    negative = {
        "EmptyRequest": {"stopped_verified": True},
        "SubmitRequest": {"preview_id": "fixture", "plan_hash": "a" * 64, "api_key": "fixture-forbidden"},
        "StartRoleRequest": {"role": "design", "workspace": "/fixture-outside"},
        "PresetInput": {"name": "fixture", "layer": {}, "admitted": True},
        "DefaultLayerInput": {"layer": {}, "account": "fixture-forbidden"},
        "PreviewRequest": {"project_id": "fixture", "goal": "fixture", "required_roles": ["design"], "token": "fixture-forbidden"},
    }
    negative["ResumeRoleRequest"] = {"role":"design", "restore":{"origin_run_id":"fixture", "checkpoint_id":"b"*64, "checkpoint_digest":"c"*64}, "native_session_id":"fixture-forbidden"}
    negative["RestoreIdentity"] = {"origin_run_id":"fixture", "checkpoint_id":"b"*64, "checkpoint_digest":"c"*64, "argv":[]}
    negative["CheckpointRef"] = {"id":"b"*64,"digest":"c"*64,"native_session_id":"fixture-forbidden"}
    for name, value in negative.items():
        assert not validator({"$ref": "#/components/schemas/" + name}).is_valid(value)
    project_response = validator({"$ref": "#/components/schemas/ProjectsReply"})
    invalid_inventory = [
        {"projects": None},
        {"projects": [{"id": "fixture", "configuration_revision": 0}]},
        {"projects": [{"id": "fixture", "configuration_revision": 1, "path": "/private"}]},
        {"projects": [{"id": "fixture", "configuration_revision": 1}], "admitted": True},
        {"projects": [{"id": "fixture", "configuration_revision": 1}] * 129},
    ]
    for value in invalid_inventory:
        assert not project_response.is_valid(value)
    task_response = validator({"$ref": "#/components/schemas/TaskListReply"})
    entry = {"id": "fixture", "project_id": "fixture", "goal": "fixture", "state": "ready", "plan_revision": 1, "generation": 0, "goal_truncated": False, "etag": '"p1-g0-ready"'}
    invalid_tasks = [
        {"tasks": None, "next_before": ""},
        {"tasks": [entry] * 33, "next_before": "fixture"},
        {"tasks": [dict(entry, goal="中" * 513)], "next_before": ""},
        {"tasks": [dict(entry, native_session_id="private")], "next_before": ""},
        {"tasks": [dict(entry, etag='W/"p1-g0-ready"')], "next_before": ""},
        {"tasks": [dict(entry, goal_truncated="true")], "next_before": ""},
        {"tasks": [], "next_before": "", "path": "/private"},
    ]
    for value in invalid_tasks:
        assert not task_response.is_valid(value)
    submission_response = validator({"$ref": "#/components/schemas/SubmissionReply"})
    valid_submission = next(s["body"]["submission"] for s in json.loads(samples.read_text()) if s["path"].endswith("/submission") and s["status"] == 200 and s["body"]["submission"] is not None)
    invalid_submissions = [
        {}, {"submission": []}, {"submission": dict(valid_submission, token="forbidden")},
        {"submission": dict(valid_submission, state="running")},
        {"submission": dict(valid_submission, state="prepared", task_id="task-unproved")},
        {"submission": dict(valid_submission, state="committed", task_id="")},
        {"submission": dict(valid_submission, key="")},
    ]
    for value in invalid_submissions:
        assert not submission_response.is_valid(value)
    return {"official_openapi_document_schema": "3.1/2022-10-07", "paths": len(EXPECTED), "operations": len(operations), "handler_samples": count, "covered_operations": len(covered), "negative_schema_cases": len(negative), "negative_project_response_cases": len(invalid_inventory), "negative_task_response_cases": len(invalid_tasks), "negative_submission_response_cases": len(invalid_submissions), "offline": True, "production_registered": False}


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--contract", type=Path, default=Path("docs/fusion/openapi-fusion.yaml"))
    parser.add_argument("--standard-schema", type=Path, required=True)
    parser.add_argument("--samples", type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(verify(args.contract, args.standard_schema, args.samples), indent=2))

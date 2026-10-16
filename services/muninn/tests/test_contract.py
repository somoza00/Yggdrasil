"""Testes de contrato do ADR-005: as fixtures em proto/events/fixtures são a
saída real do huginn (golden tests do lado Rust) e precisam validar contra o
JSON Schema e contra o modelo do muninn."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest
from jsonschema import Draft202012Validator
from pydantic import ValidationError

from muninn.models import AlertNormalized, IocKind, SeverityHint, TriageResult

EVENTS_DIR = Path(__file__).resolve().parents[3] / "proto" / "events"
SCHEMA: dict[str, Any] = json.loads((EVENTS_DIR / "normalized_event.schema.json").read_text())
FIXTURES = sorted((EVENTS_DIR / "fixtures").glob("*.json"))


def load(path: Path) -> dict[str, Any]:
    data: dict[str, Any] = json.loads(path.read_text())
    return data


def minimal() -> dict[str, Any]:
    return load(EVENTS_DIR / "fixtures" / "minimal.json")


def test_fixtures_exist() -> None:
    names = {p.name for p in FIXTURES}
    assert {"valid_cef.json", "valid_syslog.json", "minimal.json"} <= names


def test_schema_is_valid_draft_2020_12() -> None:
    Draft202012Validator.check_schema(SCHEMA)


@pytest.mark.parametrize("path", FIXTURES, ids=lambda p: p.name)
def test_fixture_validates_against_schema(path: Path) -> None:
    Draft202012Validator(SCHEMA, format_checker=Draft202012Validator.FORMAT_CHECKER).validate(
        load(path)
    )


@pytest.mark.parametrize("path", FIXTURES, ids=lambda p: p.name)
def test_fixture_parses_into_model(path: Path) -> None:
    data = load(path)
    event = AlertNormalized.model_validate(data)
    assert str(event.event_id) == data["event_id"]
    assert [(i.kind.value, i.value) for i in event.iocs] == [
        (i["kind"], i["value"]) for i in data["iocs"]
    ]


def test_model_required_fields_match_schema() -> None:
    required = {name for name, f in AlertNormalized.model_fields.items() if f.is_required()}
    assert required == set(SCHEMA["required"])


def test_model_enums_match_schema() -> None:
    props = SCHEMA["properties"]
    assert {s.value for s in SeverityHint} == set(props["severity_hint"]["enum"])
    assert {k.value for k in IocKind} == set(props["iocs"]["items"]["properties"]["kind"]["enum"])


def test_unknown_fields_are_ignored() -> None:
    data = minimal() | {"tenant": "acme", "future_field": {"x": 1}}
    data["iocs"] = [{"kind": "ip", "value": "10.0.0.1", "confidence": 0.9}]
    event = AlertNormalized.model_validate(data)
    assert not hasattr(event, "tenant")
    assert event.iocs[0].value == "10.0.0.1"


def test_newer_minor_version_is_accepted() -> None:
    event = AlertNormalized.model_validate(minimal() | {"schema_version": "1.7"})
    assert event.schema_version == "1.7"


@pytest.mark.parametrize("version", ["2.0", "0.9", "1", "1.0.0", "v1.0"])
def test_other_major_or_malformed_version_is_rejected(version: str) -> None:
    with pytest.raises(ValidationError):
        AlertNormalized.model_validate(minimal() | {"schema_version": version})


def test_unknown_ioc_kind_from_newer_minor_is_dropped() -> None:
    data = minimal()
    data["iocs"] = [
        {"kind": "email", "value": "a@evil.example.com"},
        {
            "kind": "hash",
            "value": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
        },
    ]
    event = AlertNormalized.model_validate(data)
    assert [i.kind for i in event.iocs] == [IocKind.HASH]


def test_unknown_severity_from_newer_minor_becomes_unknown() -> None:
    event = AlertNormalized.model_validate(minimal() | {"severity_hint": "catastrophic"})
    assert event.severity_hint == SeverityHint.UNKNOWN


@pytest.mark.parametrize(
    "patch",
    [
        {"event_id": "not-a-uuid"},
        {"title": ""},
        {"source": ""},
        {"parsed": "string"},
        {"iocs": [{"kind": "ip", "value": ""}]},
    ],
)
def test_invalid_values_are_rejected(patch: dict[str, Any]) -> None:
    with pytest.raises(ValidationError):
        AlertNormalized.model_validate(minimal() | patch)


@pytest.mark.parametrize("field", SCHEMA["required"])
def test_missing_required_field_is_rejected(field: str) -> None:
    data = minimal()
    del data[field]
    with pytest.raises(ValidationError):
        AlertNormalized.model_validate(data)


def test_triage_result_fixture_matches_model() -> None:
    # Mesma fixture que o ygg-api usa no teste do POST /api/v1/incidents.
    path = EVENTS_DIR.parent / "triage" / "fixtures" / "triage_result.json"
    data = load(path)
    assert TriageResult.model_validate(data).model_dump(mode="json") == data

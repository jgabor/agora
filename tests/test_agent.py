"""Tests for agent runner (consensus extraction and dry-run)."""

from __future__ import annotations

from kumbaja.agent import extract_consensus
from kumbaja.types import AgentConfig


def test_extract_consensus_present():
    content = "System A is superior [CONSENSUS: System A wins]"
    cleaned, has_consensus, statement = extract_consensus(content)
    assert has_consensus is True
    assert statement == "System A wins"
    assert "CONSENSUS" not in cleaned


def test_extract_consensus_case_insensitive():
    content = "[consensus: we agree]"
    _, has_consensus, statement = extract_consensus(content)
    assert has_consensus is True
    assert statement == "we agree"


def test_extract_consensus_missing():
    content = "No consensus here."
    _, has_consensus, _ = extract_consensus(content)
    assert has_consensus is False


def test_extract_consensus_multiline():
    content = "Line 1\n[CONSENSUS: option B is correct]\nLine 3"
    cleaned, has_consensus, statement = extract_consensus(content)
    assert has_consensus is True
    assert statement == "option B is correct"
    assert "CONSENSUS" not in cleaned
    assert "Line 1" in cleaned
    assert "Line 3" in cleaned


def test_agent_config_validation():
    agent = AgentConfig(id="test", model="openai/gpt-4")
    agent.validate()  # Should not raise

    with __import__("pytest").raises(ValueError):
        AgentConfig(id="", model="m").validate()

    with __import__("pytest").raises(ValueError):
        AgentConfig(id="a", model="").validate()

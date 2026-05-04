"""Agent runner — wraps opencode subprocess calls."""

from __future__ import annotations

import json
import subprocess
from typing import Any

from .types import AgentConfig


class AgentRunner:
    """Runs a single agent turn via opencode subprocess."""

    def __init__(self, dry_run: bool = False) -> None:
        self.dry_run = dry_run

    def run(
        self,
        agent: AgentConfig,
        envelope: dict[str, Any],
    ) -> tuple[str, dict[str, Any]]:
        """Call opencode run with the given agent and envelope.

        Returns (content, metadata) where metadata contains tokens and cost.
        """
        if self.dry_run:
            return self._dry_run_response(agent, envelope)

        payload = f"{agent.system_prompt}\n\n{json.dumps(envelope)}"
        cmd = [
            "opencode",
            "run",
            "--model", agent.model,
            "--format", "json",
            "--dangerously-skip-permissions",
        ]

        proc = subprocess.run(
            cmd,
            input=payload,
            capture_output=True,
            text=True,
        )

        if proc.returncode != 0:
            raise RuntimeError(
                f"opencode run failed (exit {proc.returncode}): "
                f"{proc.stderr.strip() or proc.stdout.strip()}"
            )

        text_parts: list[str] = []
        metadata: dict[str, Any] = {"tokens": {}, "cost": None}

        for line in proc.stdout.strip().splitlines():
            line = line.strip()
            if not line:
                continue
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                continue

            event_type = event.get("type")
            part = event.get("part", {})

            if event_type == "text":
                text_parts.append(part.get("text", ""))
            elif event_type == "error":
                raise RuntimeError(f"opencode run error: {event.get('error', event)}")
            elif event_type == "step_finish":
                metadata["tokens"] = part.get("tokens", {})
                metadata["cost"] = part.get("cost")

        content = "".join(text_parts).strip()
        if not content:
            raise RuntimeError("Agent produced empty text response")

        return content, metadata

    def _dry_run_response(
        self,
        agent: AgentConfig,
        envelope: dict[str, Any],
    ) -> tuple[str, dict[str, Any]]:
        """Return a canned response for dry-run mode."""
        topic = envelope.get("topic", "unknown topic")
        return (
            f"[DRY RUN] Agent '{agent.id}' responds to: {topic}",
            {"tokens": {"total": 100, "input": 50, "output": 50}, "cost": 0.001},
        )


def extract_consensus(content: str) -> tuple[str, bool, str]:
    """Extract consensus marker from agent response.

    Returns (cleaned_content, has_consensus, consensus_statement).
    Consensus is signaled with: [CONSENSUS: <statement>]
    """
    import re

    pattern = re.compile(
        r"\[CONSENSUS\s*:\s*(.*?)\]",
        re.IGNORECASE | re.DOTALL,
    )
    match = pattern.search(content)
    if not match:
        return content, False, ""

    consensus_statement = match.group(1).strip()
    cleaned = pattern.sub("", content).strip()
    return cleaned, True, consensus_statement

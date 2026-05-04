"""Final synthesis engine — generates a structured summary of a deliberation."""

from __future__ import annotations

import json
from typing import Any

from .agent import AgentRunner
from .types import AgentConfig, DeliberationConfig, TurnRecord

SYNTHESIS_SYSTEM_PROMPT = """\
You are a deliberation synthesis agent. Your job is to read the full transcript
of a multi-agent deliberation and produce a structured summary.

Your output must be valid JSON with this exact structure:
{
  "key_arguments": ["argument 1", "argument 2", ...],
  "points_of_agreement": ["agreement 1", ...],
  "unresolved_tensions": ["tension 1", ...],
  "recommended_decision": "...",
  "confidence": "high|medium|low"
}

Be concise but thorough. Capture the essential insights from the deliberation.
"""


class SynthesisEngine:
    """Generates a final synthesis from a deliberation transcript."""

    def __init__(self, runner: AgentRunner) -> None:
        self.runner = runner

    def synthesize(
        self,
        records: list[TurnRecord],
        topic: str,
        config: DeliberationConfig,
    ) -> dict[str, Any]:
        """Run a synthesis agent to summarize the deliberation.

        Returns the parsed JSON summary.
        """
        transcript_text = self._format_transcript(records)
        envelope = {
            "topic": topic,
            "transcript": transcript_text,
            "num_agents": len(config.agents),
            "total_turns": len([r for r in records if r.agent_id != "orchestrator"]),
        }

        # Determine synthesis model
        model = config.synthesis_model or config.agents[0].model
        synth_agent = AgentConfig(
            id="synthesizer",
            model=model,
            system_prompt=SYNTHESIS_SYSTEM_PROMPT,
        )

        try:
            content, _ = self.runner.run(synth_agent, envelope)
            # Try to extract JSON from the response
            return self._extract_json(content)
        except Exception as exc:
            return {
                "key_arguments": [],
                "points_of_agreement": [],
                "unresolved_tensions": [],
                "recommended_decision": f"Synthesis failed: {exc}",
                "confidence": "low",
            }

    def _format_transcript(self, records: list[TurnRecord]) -> str:
        lines: list[str] = []
        for r in records:
            lines.append(f"[Turn {r.turn}] {r.agent_id}: {r.content}")
        return "\n".join(lines)

    def _extract_json(self, content: str) -> dict[str, Any]:
        """Extract JSON from potentially markdown-wrapped response."""
        import re

        # Look for JSON in code blocks
        code_block = re.search(r"```(?:json)?\s*(\{.*?\})\s*```", content, re.DOTALL)
        if code_block:
            content = code_block.group(1)

        # Find the first { and last }
        start = content.find("{")
        end = content.rfind("}")
        if start >= 0 and end > start:
            content = content[start : end + 1]

        return json.loads(content)

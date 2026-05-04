#!/usr/bin/env python3
"""
Closed-loop multi-agent deliberation system.

A minimal Python orchestrator that spins up N heterogeneous agents via
`opencode run`, passes them around a ring topology, and logs the raw
transcript to a JSONL file.
"""

import argparse
import json
import subprocess
import sys
import time
from pathlib import Path

import yaml


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Closed-loop multi-agent deliberation via opencode run"
    )
    parser.add_argument(
        "--config", required=True, help="Path to YAML agent configuration file"
    )
    parser.add_argument(
        "--topic", required=True, help="Topic or goal for deliberation"
    )
    parser.add_argument(
        "--time", type=int, required=True, help="Time limit in seconds"
    )
    parser.add_argument(
        "--window", type=int, required=True, help="Number of predecessor messages each agent sees"
    )
    parser.add_argument(
        "--max-turns", type=int, required=True, help="Maximum total turns across all agents"
    )
    parser.add_argument(
        "--output", required=True, help="Path to write the JSONL transcript"
    )
    parser.add_argument(
        "--verbose", action="store_true", help="Print each agent's response to stdout in real-time"
    )
    return parser.parse_args()


def load_config(path: str) -> list[dict]:
    with open(path, "r") as f:
        data = yaml.safe_load(f)
    agents = data.get("agents", [])
    if not agents:
        raise ValueError("Configuration must contain at least one agent")
    for i, agent in enumerate(agents):
        if "id" not in agent or "model" not in agent:
            raise ValueError(f"Agent {i} missing required 'id' or 'model' field")
    return agents


def run_agent(model: str, system_prompt: str, envelope: dict) -> tuple[str, dict]:
    """
    Call `opencode run` with the given model and pipe the envelope as stdin.
    Returns the extracted text content and usage metadata.
    """
    payload = f"{system_prompt}\n\n{json.dumps(envelope)}"
    cmd = [
        "opencode", "run",
        "--model", model,
        "--pure",
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
            f"opencode run failed (exit {proc.returncode}): {proc.stderr.strip() or proc.stdout.strip()}"
        )

    text_parts = []
    metadata = {"tokens": {}, "cost": None}

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


def main() -> int:
    args = parse_args()
    agents = load_config(args.config)
    num_agents = len(agents)

    transcript: list[dict] = []
    turn = 0
    start_time = time.time()

    # Orchestrator seed message
    seed_msg = f"Begin deliberating on the following topic: {args.topic}"
    transcript.append({
        "turn": -1,
        "agent_id": "orchestrator",
        "model": None,
        "timestamp": start_time,
        "content": seed_msg,
        "tokens": {},
        "cost": None,
    })

    print(f"Deliberation started: {args.topic}")
    print(f"Agents: {', '.join(a['id'] for a in agents)}")
    print(f"Time limit: {args.time}s | Max turns: {args.max_turns} | Window: {args.window}")
    print(f"Output: {args.output}")
    print("-" * 60)

    try:
        while turn < args.max_turns:
            elapsed = time.time() - start_time
            if elapsed >= args.time:
                print("\nTime limit reached.")
                break

            agent = agents[turn % num_agents]
            agent_id = agent["id"]
            model = agent["model"]
            system_prompt = agent.get("system_prompt", "")

            # Determine predecessor
            predecessor_idx = (turn - 1) % num_agents if turn > 0 else -1
            if predecessor_idx == -1:
                predecessor_id = "orchestrator"
            else:
                predecessor_id = agents[predecessor_idx]["id"]

            # Gather last K messages from predecessor
            history = []
            for msg in reversed(transcript):
                if msg["agent_id"] == predecessor_id:
                    history.append({"agent_id": msg["agent_id"], "content": msg["content"]})
                if len(history) >= args.window:
                    break
            history.reverse()

            envelope = {
                "topic": args.topic,
                "history": history,
            }

            turn_start = time.time()
            content, metadata = run_agent(model, system_prompt, envelope)
            turn_duration = time.time() - turn_start

            tokens = metadata.get("tokens", {})
            total_tokens = tokens.get("total", "?")
            cost = metadata.get("cost")
            cost_str = f"${cost:.6f}" if cost is not None else "?"

            transcript.append({
                "turn": turn,
                "agent_id": agent_id,
                "model": model,
                "timestamp": time.time(),
                "content": content,
                "tokens": tokens,
                "cost": cost,
            })

            progress = f"[{turn + 1}/{args.max_turns}] {agent_id} ({model}) · {turn_duration:.1f}s · {total_tokens}tok · {cost_str}"
            print(progress)
            if args.verbose:
                print()
                for line in content.splitlines():
                    print(f"    {line}")
                print()
                print("-" * 60)

            turn += 1

        else:
            print(f"\nMax turns ({args.max_turns}) reached.")

    except RuntimeError as exc:
        print(f"\nHALT: {exc}")
        return 1
    finally:
        output_path = Path(args.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        with open(output_path, "w") as f:
            for msg in transcript:
                f.write(json.dumps(msg, ensure_ascii=False) + "\n")
        print(f"\nTranscript written to {args.output} ({len(transcript)} entries)")

    return 0


if __name__ == "__main__":
    sys.exit(main())

import json
from collections.abc import Callable
from dataclasses import replace
from datetime import datetime, timezone
from pathlib import Path

from tattva import providers
from tattva.project import write_file_atomic
from tattva.providers import Call, CallFailed

REPAIR_PROMPT = (
    "You repair JSON answers that failed validation. You receive the original request, the answer that was given, "
    "and the rules the answer breaks. Return the corrected answer, matching the same schema, and change only what "
    "is needed to satisfy the rules. Never drop a behaviour, a checkpoint or a step to make the answer fit; split "
    "or move things instead.")


class InvalidOutput(CallFailed):
    """Output that still broke the rules after one repair."""

    def __init__(self, output: dict, errors: list[str], cost: float | None, saved: Path | None):
        message = "generated output broke the curriculum rules even after a repair:\n  " + "\n  ".join(errors)
        if saved:
            message += f"\nThe output is saved in {saved}"
        super().__init__(message, cost)
        self.output, self.errors, self.saved = output, errors, saved


def add_cost(a: float | None, b: float | None) -> float | None:
    return None if a is None or b is None else a + b


def _save_failed(failed_dir: Path, label: str, output: dict, errors: list[str]) -> Path:
    stamp = datetime.now(timezone.utc).strftime("%Y%m%d-%H%M%S")
    path = failed_dir / f"{label.replace(' ', '-')}-{stamp}.json"
    write_file_atomic(path, (json.dumps({"errors": errors, "output": output}, indent=2) + "\n").encode())
    return path


def generate(call: Call, check: Callable[[dict], list[str]],
             failed_dir: Path | None) -> tuple[dict, float | None, list[str]]:
    """One call, check it, and at most one repair call. Returns (output, total cost, rules the first draft broke)."""
    first = providers.run_call(call)
    errs = check(first.output)
    if not errs:
        return first.output, first.cost, []
    prompt = ("The answer below breaks these rules:\n- " + "\n- ".join(errs) + "\n\nOriginal request:\n"
              + call.prompt + "\n\nAnswer:\n" + json.dumps(first.output, indent=2))
    repair = replace(call, system=REPAIR_PROMPT, prompt=prompt, label=call.label + " repair", web=False)
    try:
        fixed = providers.run_call(repair)
    except CallFailed as e:
        e.cost = add_cost(first.cost, e.cost)
        raise
    cost = add_cost(first.cost, fixed.cost)
    if still := check(fixed.output):
        saved = _save_failed(failed_dir, call.label, fixed.output, still) if failed_dir else None
        raise InvalidOutput(fixed.output, still, cost, saved)
    return fixed.output, cost, errs

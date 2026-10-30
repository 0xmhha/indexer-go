#!/usr/bin/env python3
"""One durable core pass; missing/skipped cases are a failed smoke run."""
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
CASES = (
    "TestExecutionPublicationSevenPartsFreshAndAtomicVisibility",
    "TestExecutionPublicationFailureNeverPublishesOrLeavesFeature",
    "TestExecutionPublicationIncompleteAndGenerationRejectedBeforeCallbacks",
    "TestExecutionPublicationPostFreshGenerationFenceAndQueuedError",
    "TestExecutionConfirmationLostACKAndCanonicalOlderGeneration",
    "TestExecutionConfirmationFreshFailureFenceAndUnpublished",
    "TestExecutionReadSnapshotUsesReservedFourthSlotAndRejectsWrites",
    "TestExecutionReadSnapshotCancellationJoinsActiveSQL",
    "TestExecutionReorgCheckFencesBeforeTargetSearchSurvivesRestartAndConvertsAtomically",
    "TestExecutionReorgCheckCorruptFenceCannotBecomeRewind",
    "TestExecutionRewindFreshFailureKeepsBlockedOriginalData",
)


def validate(output):
    events = []
    for line in output.splitlines():
        if line.startswith("{"):
            events.append(json.loads(line))
    passed = {e.get("Test") for e in events if e.get("Action") == "pass"}
    bad = [e.get("Test") for e in events if e.get("Action") in ("fail", "skip") and e.get("Test") in CASES]
    missing = set(CASES) - passed
    if missing or bad:
        raise ValueError(f"core smoke missing={sorted(missing)}, failed/skipped={bad}")


def main():
    command = [str(ROOT / "scripts/test-postgres.sh"), "./pkg/storage/postgres", "-short", "-json", "-count=1", "-timeout=10m", "-run", "^(" + "|".join(CASES) + ")$"]
    result = subprocess.run(command, cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
    sys.stdout.write(result.stdout)
    if result.returncode:
        return result.returncode
    try:
        validate(result.stdout)
    except (ValueError, json.JSONDecodeError) as error:
        print(error, file=sys.stderr)
        return 1
    print(f"durable core smoke: {len(CASES)} actual named passes; no skipped cases")
    return 0


if __name__ == "__main__":
    sys.exit(main())

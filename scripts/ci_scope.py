#!/usr/bin/env python3
"""Conservative PR scope; unknown or unavailable diffs run code checks."""
import argparse
import json
from pathlib import PurePosixPath, Path
import re
import subprocess


def classify(paths, full=False):
    def valid(path):
        return isinstance(path, str) and path and not path.startswith("/") and not any(
            part in ("", ".", "..") for part in path.split("/")
        ) and not any(ord(char) < 32 for char in path)

    def document(path):
        parsed = PurePosixPath(path)
        suffix = parsed.suffix.lower()
        if "testdata" in parsed.parts or parsed.name.lower().startswith("requirements"):
            return False
        if path in ("LICENSE", "NOTICE", "COPYING"):
            return True
        if path.startswith("docs/"):
            return suffix in (".md", ".rst", ".txt", ".png", ".jpg", ".jpeg", ".svg", ".webp", ".gif")
        return suffix in (".md", ".rst") and (
            len(parsed.parts) == 1 or parsed.stem.upper() in ("README", "CONTRIBUTING", "CLAUDE", "CHANGELOG", "LICENSE", "NOTICE", "COPYING")
        )

    unknown = not paths or any(not valid(path) for path in paths)
    docs = not full and not unknown and all(document(path) for path in paths)
    native = full or unknown or any(path.startswith("x/arc/executor/") and not document(path) for path in paths)
    return {"code": not docs, "arc": bool(native), "full": bool(full), "docs_only": bool(docs)}


def changed(base, head):
    if not re.fullmatch(r"[0-9a-f]{40,64}", base or "") or not re.fullmatch(r"[0-9a-f]{40,64}", head or ""):
        return None
    result = subprocess.run(["git", "diff", "--name-only", "--no-renames", "-z", base, head], capture_output=True)
    if result.returncode:
        return None
    return [p.decode("utf-8", errors="surrogateescape") for p in result.stdout.split(b"\0") if p]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", default="")
    parser.add_argument("--head", default="")
    parser.add_argument("--full", choices=["true", "false"], default="false")
    parser.add_argument("--github-output", type=Path)
    args = parser.parse_args()
    result = classify(changed(args.base, args.head), args.full == "true")
    print(json.dumps(result, sort_keys=True))
    if args.github_output:
        with args.github_output.open("a", encoding="utf-8") as output:
            for name, value in result.items():
                output.write(f"{name}={str(value).lower()}\n")


if __name__ == "__main__":
    main()

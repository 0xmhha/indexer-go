#!/usr/bin/env python3
"""Check changed Markdown and maintained entry points without Go/Rust/Docker."""
import argparse
from collections import Counter
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import unquote, urlsplit

from ci_scope import changed

LINK = re.compile(r"\[[^\]]*\]\(\s*(?:<([^>]+)>|([^\s)]+))(?:\s+['\"][^)]*)?\s*\)")


def prose(text):
    lines, fence = [], None
    for line in text.splitlines():
        match = re.match(r"\s*(```+|~~~+)", line)
        if match:
            token = match[1][0]
            fence = None if fence == token else token if fence is None else fence
        elif fence is None:
            lines.append(line)
    return "\n".join(lines)


def anchors(text):
    result = set(re.findall(r"(?:id|name)=[\"']([^\"']+)[\"']", text))
    counts = Counter()
    for heading in re.findall(r"^#{1,6}\s+(.+?)\s*#*\s*$", prose(text), re.M):
        heading = re.sub(r"\[([^]]+)\]\([^)]*\)", r"\1", heading)
        slug = "".join(c for c in heading.lower() if c.isalnum() or c in " _-").replace(" ", "-")
        result.add(slug if counts[slug] == 0 else f"{slug}-{counts[slug]}")
        counts[slug] += 1
    return result


def targets(document):
    for match in LINK.finditer(prose(document.read_text(encoding="utf-8"))):
        url = urlsplit(match[1] or match[2])
        if url.scheme or url.netloc:
            continue
        path = unquote(url.path)
        yield path, unquote(url.fragment)


def destination(root, document, path):
    return (root / path.lstrip("/") if path.startswith("/") else document.parent / path).resolve() if path else document.resolve()


def check(root, files):
    root = root.resolve()
    errors, links = [], 0
    for file in sorted(files):
        document = root / file
        if not document.is_file() or document.suffix.lower() != ".md":
            continue
        if not document.resolve().is_relative_to(root):
            errors.append(f"{file}: document leaves repository")
            continue
        for path, fragment in targets(document):
            links += 1
            target = destination(root, document, path)
            if not target.is_relative_to(root):
                errors.append(f"{file}: link leaves repository: {path}")
            elif not target.exists():
                errors.append(f"{file}: missing link: {path}")
            elif fragment and target.suffix.lower() == ".md" and fragment not in anchors(target.read_text(encoding="utf-8")):
                errors.append(f"{file}: missing anchor: {path}#{fragment}")
    return links, errors


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--base", default="")
    parser.add_argument("--head", default="")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[1]
    paths = changed(args.base, args.head) or []
    selected = {"README.md", "CONTRIBUTING.md", "CLAUDE.md", "docs/README.md"}
    selected.update(path for path in paths if path.lower().endswith(".md"))
    deleted = {(root / path).resolve() for path in paths if not (root / path).exists()}
    if deleted:
        tracked = subprocess.check_output(["git", "ls-files", "-z", "*.md"], cwd=root).split(b"\0")
        for name in tracked:
            if not name:
                continue
            path = name.decode()
            document = root / path
            if document.is_file() and document.resolve().is_relative_to(root) and any(destination(root, document, target) in deleted for target, _ in targets(document) if target):
                selected.add(path)
    links, errors = check(root, selected)
    for error in errors:
        print(error, file=sys.stderr)
    print(f"documentation: {len(selected)} selected files, {links} relative links, {len(errors)} errors")
    return bool(errors)


if __name__ == "__main__":
    sys.exit(main())

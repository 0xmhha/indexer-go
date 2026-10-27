#!/usr/bin/env python3
"""Read-only, fail-closed observer for the four ordered Seed releases.

Run from any directory: python3 -B verification/verify_release_sequence.py
Requires git, gh authentication, Go and the repository's native test prerequisites.
Missing releases/checks/tests are failures; an empty ledger is never completion.
"""
import json
import pathlib
import datetime
import re
import subprocess
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
CHECKS = {"lint", "test", "postgres", "examples", "arc"}
TESTS = [
    "TestSeedPriority1BlobSupplyVerification",
    "TestSeedPriority2CallQueriesAndOrphanTraces",
    "TestSeedPriority3E6CryptographicConsensus",
    "TestSeedPriority4NativeReclamationBackupRestore",
]


def run(*args):
    return subprocess.check_output(args, cwd=ROOT, text=True)


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sync_status(sync):
    """Accept the exact predecessor receipt spellings; absence is never clean."""
    names = [name for name in ("status", "primary_status") if name in sync]
    require(bool(names), "synchronization receipt omits primary status")
    require(all(sync[name] == "" for name in names), "synchronization receipt is dirty or conflicting")
    return ""


def owned_run_paths(release, validation):
    """Every cleanup generation must be scoped and present in reviewed docs."""
    additional = release.get("additional_owned_run_dirs", [])
    require(isinstance(additional, list), "additional owned runs must be a list")
    names = [release["owned_run_dir"], *additional]
    require(all(isinstance(name, str) for name in names), "owned run path must be a string")
    require(len(set(names)) == len(names), "duplicate owned run paths")
    paths = []
    for name in names:
        path = pathlib.Path(name)
        require(path.is_absolute() and path.parent == pathlib.Path("/private/tmp")
                and path.name.startswith("indexer-four.") and path.name != "indexer-four."
                and str(path) == name,
                "cache cleanup ownership is not scoped")
        require(name in validation, "owned run path differs from reviewed validation evidence")
        paths.append(path)
    return paths


def observe():
    path = ROOT / "verification/releases.json"
    require(path.is_file(), "release ledger is missing; no release completion established")
    releases = json.loads(path.read_text())
    require(isinstance(releases, list) and len(releases) == 4,
            "all four ordered releases are required")
    previous = None
    require(len({release["pr"] for release in releases}) == 4, "release PRs must be distinct")
    reflog = run("git", "reflog", "show", "--date=unix", "--format=%H%x09%gd", "main")
    sync_times = {}
    for line in reflog.splitlines():
        sha, selector = line.split("\t", 1)
        match = re.search(r"@\{(\d+)\}$", selector)
        if match:
            sync_times.setdefault(sha, []).append(int(match.group(1)))
    for priority, release in enumerate(releases, 1):
        require(release["priority"] == priority, "release order differs from Seed")
        pr = json.loads(run("gh", "pr", "view", str(release["pr"]), "--json",
                            "number,state,isDraft,baseRefName,headRefName,headRefOid,mergeCommit,mergedAt,commits,url"))
        require(pr["state"] == "MERGED" and not pr["isDraft"], f"priority {priority}: PR not released")
        require(pr["baseRefName"] == "main" and pr["headRefName"] == release["branch"]
                and release["branch"].startswith("refactor/"), "wrong release branch")
        sha = pr["mergeCommit"]["oid"]
        require(sha == release["main_sha"], "merged main SHA differs")
        require(pr["headRefOid"] == release["head_sha"], "released head differs")
        # A squash release has one parent and retains the final PR tree.
        parents = run("git", "rev-list", "--parents", "-n", "1", sha).split()
        require(len(parents) == 2, "release is not a one-parent squash commit")
        require(run("git", "rev-parse", sha + "^{tree}").strip() ==
                run("git", "rev-parse", release["head_sha"] + "^{tree}").strip(),
                "squash tree differs from reviewed head")
        run("git", "merge-base", "--is-ancestor", sha, "origin/main")
        run("git", "ls-remote", "--exit-code", "origin", "refs/heads/" + release["branch"])
        pages = json.loads(run("gh", "api", "--paginate", "--slurp",
                              f"repos/0xmhha/indexer-go/commits/{release['head_sha']}/check-runs?per_page=100"))
        checks = [check for page in pages for check in page["check_runs"]]
        for name in CHECKS:
            matching = [check for check in checks if check["name"] == name
                        and check["head_sha"] == release["head_sha"]
                        and check["app"]["slug"] == "github-actions"]
            require(matching, f"priority {priority}: missing exact-head {name}")
            latest = max(matching, key=lambda check: check["id"])
            require(latest["status"] == "completed" and latest["conclusion"] == "success",
                    f"priority {priority}: {name} did not succeed")
            require(latest["completed_at"] <= pr["mergedAt"], "merge preceded CI success")
        if previous is not None:
            require(previous["mergedAt"] < pr["mergedAt"], "release timestamps out of order")
            # The first implementation commit must descend from the previous
            # released main, not from an unmerged stacked feature branch.
            first = pr["commits"][0]["oid"]
            run("git", "merge-base", "--is-ancestor", previous["mergeCommit"]["oid"], first + "^")
        evidence = ROOT / release["sync_evidence"]
        sync = json.loads(evidence.read_text())
        require(sync["main"] == sha and sync["origin_main"] == sha and sync_status(sync) == "",
                "main synchronization evidence is dirty or mismatched")
        require(pr["mergedAt"] <= sync["observed_at"], "sync evidence predates merge")
        # The ledger is not authority for a historical main update: cross-check
        # the actual shared Git reflog and the next release's immutable ancestry.
        merged_at = int(datetime.datetime.fromisoformat(pr["mergedAt"].replace("Z", "+00:00")).timestamp())
        observed_at = int(datetime.datetime.fromisoformat(sync["observed_at"].replace("Z", "+00:00")).timestamp())
        require(any(merged_at <= when <= observed_at for when in sync_times.get(sha, [])),
                "main reflog does not establish synchronization after release")
        if previous is not None:
            require(previous["sync_observed_at"] <= pr["commits"][0]["committedDate"],
                    "next implementation committed before clean main sync")
        pr["sync_observed_at"] = sync["observed_at"]
        previous = pr
        validation = run("git", "show", release["head_sha"] + ":" + release["validation_path"])
        for run_dir in owned_run_paths(release, validation):
            for name in ("go-cache", "lint-cache"):
                cache = run_dir / name
                require(not cache.exists() and not cache.is_symlink(), "execution-owned cache remains: " + str(cache))
        # Demand an actual named pass. Zero matching tests or a skipped feature
        # is a failure, even when `go test` exits successfully.
        output = run("go", "test", "-json", "-count=1", "./...", "-run", "^" + TESTS[priority - 1] + "$")
        events = [json.loads(line) for line in output.splitlines() if line.startswith("{")]
        require(any(event.get("Action") == "pass" and event.get("Test") == TESTS[priority - 1]
                    for event in events), "feature test did not actually execute")
        require(not any(event.get("Action") in {"fail", "skip"} and event.get("Test", "").startswith(TESTS[priority - 1])
                        for event in events), "feature verification failed or was exempted")
        print(f"priority {priority}: {pr['url']} @ {sha}: CI5, squash, sync, feature verification passed")
    require(run("git", "rev-parse", "main").strip() == run("git", "rev-parse", "origin/main").strip(),
            "main is not synchronized")
    remote_main = run("git", "ls-remote", "origin", "refs/heads/main").split()[0]
    require(remote_main == releases[-1]["main_sha"], "remote main differs from final release")
    worktrees = run("git", "worktree", "list", "--porcelain").split("\n\n")
    main_trees = [record for record in worktrees if "branch refs/heads/main" in record.splitlines()]
    require(len(main_trees) == 1, "main worktree cannot be independently located")
    main_path = main_trees[0].splitlines()[0].removeprefix("worktree ")
    status = subprocess.check_output(["git", "-C", main_path, "status", "--porcelain", "--untracked-files=all"], text=True)
    require(status == "", "final main worktree is dirty")
    require(subprocess.check_output(["git", "-C", main_path, "rev-parse", "HEAD"], text=True).strip() == remote_main,
            "final main worktree is not synchronized to remote main")
    report = (ROOT / "verification/completion.ko.md").read_text()
    require(any("가" <= character <= "힣" for character in report), "Korean completion report is missing")
    require(all(str(release["pr"]) in report for release in releases) and releases[-1]["main_sha"] in report,
            "completion report omits releases or final main SHA")
    require("캐시" in report and "보류" in report and "테스트" in report, "completion report omits required outcomes")
    print("four ordered releases verified")


if __name__ == "__main__":
    try:
        observe()
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError, json.JSONDecodeError) as error:
        print(f"release verification FAILED: {error}", file=sys.stderr)
        sys.exit(1)

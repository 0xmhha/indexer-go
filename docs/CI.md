# CI checks for developers

PRs use short checks once. Merging a checked PR does not automatically repeat
the same suite on main ([#247](https://github.com/0xmhha/indexer-go/pull/247)).
Full regression is an explicit operation, not a
prerequisite for every documentation or small code change.

| Change | Default checks |
| --- | --- |
| Root Markdown, maintained documentation or documentation images only | Relative links, anchors and CI policy; no Go/Rust/Docker setup |
| Go, SQL, config, workflow or unknown code path | Build/vet/lint, one root/wire/StableNet short functional pass, examples, offline config checks and durable DB core smoke |
| Arc executor Rust/Cargo | The code checks plus pinned cached native compilation and bounded frame/read-view/unwind smoke |
| Unavailable/invalid diff | Conservative code/native checks; never assume documentation-only |

The existing `lint`, `test`, `postgres`, `examples` and `arc` names remain. A small
scope job classifies exact base/head paths. Non-applicable jobs report their
scope without starting a compiler or service. Renames/deletes, workflow/config
changes and mixed documentation/code changes cannot hide code from checks.
Text dependencies and runtime/test fixtures remain code, even with a Markdown
or text extension. Unknown nested documents conservatively run code checks.

## Run short checks locally

```sh
make test-ci-short
make test-ci-postgres-short
python3 -B -m unittest discover -s verification -p 'test_ci_*.py'
python3 -B scripts/check_docs.py --base BASE_SHA --head HEAD_SHA
```

Short tests run once. The PostgreSQL smoke uses its own named disposable
fsync-on container/volume and verifies actual named passes; missing or skipped
cases fail. It covers seven-part atomic visibility, failed/mandatory final fresh,
post-response generation fence, lost/exact ACK, reserved fourth connection,
cancel/join and durable reorg recovery. Shared physical RPC budget checks remain
in the short Go suite. No pool, RPC or commit/ACK semantics change.

## Request full qualification

In GitHub Actions, select **CI → Run workflow**, choose the intended branch and
set **full=true**. This retains the full suites, individual CPU1/race/count200
budgets, all four separate state-proof acceptance cases, native SDK/SIGKILL/COW,
fixture parity and backup admission. No scheduled full runs are added.

Use the full mode or relevant local full checks for releases and native/storage/
concurrency changes that need that evidence. New concurrency code still needs
its targeted local single200; a documentation edit does not. Public RPC, node
services, operational data, provisioning and performance checks remain opt-in.

Native dependencies/target are cached by OS, pinned Rust and Cargo.lock. A cold
native build can still take time; ordinary Go/documentation PRs avoid it entirely.
Workflow/job durations are evidence of CI execution, not indexer throughput.

The first released short-code PR, [#246](https://github.com/0xmhha/indexer-go/pull/246),
passed [CI321](https://github.com/0xmhha/indexer-go/actions/runs/40989226640):
7 minutes 12 seconds from the first job start to the last job completion,
excluding queue time. PostgreSQL took 45 seconds including setup and cleanup;
all eleven core cases actually passed. This is one observed run, not a promised
duration for every change or a cold native build.

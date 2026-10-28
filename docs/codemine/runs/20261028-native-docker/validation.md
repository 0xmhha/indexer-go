# Native Docker diagnostic validation

Scope is the separate diagnostic image/Compose declarations and offline checker.
No Go runtime, RPC, DB pool or writer code changes. No Docker/node/operating DB,
disk probe/provisioning, image build/pull, performance or host-load work.

The first compiling Python test failed its requirement assertion: the draft
returned PASS instead of HOLD. The legacy recipe check also failed a concrete
missing `COPY execwire/` assertion before the input-order fix was restored.
Actual host compilation exposed CGO0's unsupported `blst` build; CGO1 preserves
the existing crypto implementation. The four host binary/Compose fixture then
passed, including both product entrypoints and source/native path bindings.

Review REDs demonstrated a credentialed malformed image being masked by HOLD,
and a closed stdout pipe returning Python exit120. The corrected image grammar
and explicit CLI flush/close return FAIL/1 without raw input or a traceback.
The full fixture checks include deterministic statuses, bounds, unsafe topology,
no environment/Docker access and byte/inode preservation. New concurrent runtime
state is absent; no new count200 campaign or performance claim is made.

Final source-bound contributor and release evidence is recorded separately;
only actual successful exits and the final exact-head CI5 qualify for release.
Host-library mocks do not prove image ABI, Docker Desktop sharing/UIDs, APFS,
peer, authenticated state or operational readiness.

The final [source manifest](../../../../verification/native-docker/source.json)
and [command receipts](../../../../verification/native-docker/checks.json) bind
all11 successful local gates to unchanged executable inputs. The Go source hash
remains e4ec28b6fcf8181f0fb79baeb953e9e4695efeaa251b3e31782a0330bb7cba38.
The full root/wire/StableNet executor race target ran once at GOMAXPROCS2,
package parallelism2 and full20m package timeout. CLI fixtures used PyYAML6.0.3
and all13 cases, including actual four-binary bindings, without a skipped case.
No new Go/SQL changes or concurrent runtime state warrant a local PostgreSQL
rerun or new200 campaign. Required final-head PostgreSQL/other CI jobs still
must pass before merge. Local test processes receive no inherited operator
INDEXER/PG environment values. Owned host-binary fixture directories are removed
on exit. Logs remain in this attempt's private temporary directory; receipts
retain their SHA256 and exact command/exit rather than committing full logs.

[Secret receipts](../../../../verification/native-docker/secrets.json) record
staged findings0 and history15 reviewed public fixtures, unreviewed0. Raw history
scanner exit1 is not represented as zero findings. [RED observations](../../../../verification/native-docker/red.txt)
retain the specific pre-correction assertion failures. The commit/PR and Git
metadata carry final publication identity; only the actual final-head CI5 result
authorizes the source merge, never operational activation.

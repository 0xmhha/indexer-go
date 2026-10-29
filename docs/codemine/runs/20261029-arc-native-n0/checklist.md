# N0 checklist

- [x] Record approved requirements, immutable pins and owned build/fixture scope.
- [x] Write bounded framing/fence tests and real SDK qualification tests.
- [x] Compile initial tests; compile errors are not behavioral RED evidence.
- [x] Record actual framing RED, implement and prove GREEN.
- [x] Record actual durable-fence RED, implement and prove GREEN.
- [x] Prove actual native-v2 Full persistence, RO refresh and reopen.
- [x] Prove non-genesis full checkpoint/root/preimage coverage.
- [x] Prove healthy unwind/reimport and interrupted save/unwind fence behavior.
- [x] Complete relevant local Rust and unchanged Go source/evidence checks.
- [x] Update current docs and n/n-1/n-2; verify links and leak scans.
- [x] Define and enforce exact-head CI5/squash/main release gate before N1.

Actual CI/merge/main evidence is recorded in the release PR metadata and private
continuation ledger. This source checklist does not pre-certify a future CI run.

N1-N6 remain pending; no operational activation or performance evidence is claimed.

[Validation](validation.md) separates actual RED/GREEN from probes and held work.

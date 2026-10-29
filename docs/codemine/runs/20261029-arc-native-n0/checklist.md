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

Actual [N0 release evidence](../../../../verification/native-import-n0/release.json):
PR #241 head cc4517755e312a297571da9a1c1bee723b439bba passed CI5 run40858712523
and squash/main223f61735a343bf730560151617dd2f4616b66e5 was synchronized.
This receipt records completed events; it does not pre-certify later CI runs.

Current implementation status is in the [central checklist](../20261029-arc-first-import/checklist.md); no operational activation or performance evidence is claimed.

[Validation](validation.md) separates actual RED/GREEN from probes and held work.

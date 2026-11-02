# 세션 재개와 인계 규약

새 세션은 채팅 이력이나 이전 머신의 `/private/tmp` 없이 현재 작업을 판단할
수 있어야 한다. 저장소의 현재 상태는 덮어써 최신으로 유지하고, 과거 과정은
Git history와 해당 기능의 설계·검증 기록으로 확인한다. 이 규약 자체는
서비스 실행이나 운영 HOLD 해제를 허용하지 않는다.

## 정본과 역할

| 문서·근거 | 소유하는 정보 |
| --- | --- |
| [AGENTS.md](../AGENTS.md) | 도구가 읽을 수 있는 고정 시작 절차와 작업 경계 |
| [SESSION_STATE.md](SESSION_STATE.md) | 현재 범위·다음 작업 ID·직전 세 기능 맥락·검증된 릴리스 기준점 |
| [남은 작업](analysis/open-items.md) | 안정적인 작업 ID·우선순위·상태·의존성·완료 조건 |
| [운영 준비](OPERATIONS_READINESS.md) | 실제 운영 입력의 관측 시점·권장값·미확정/HOLD |
| 기능별 plan/checklist/validation | 수용한 의도·RED→GREEN·검증 범위·설계 결정 |
| 실제 Git·PR·CI | 현재 파일/커밋·머지 여부·검사 결과의 증거 |

현재 작업/다음 작업을 CLAUDE나 여러 가이드에 각각 복제하지 않는다.
[CLAUDE.md](../CLAUDE.md)는 아키텍처와 구현 계약을 설명하고 현재 상태는
SESSION_STATE로 연결한다. 운영 입력을 그 문서로 옮겨 중복 관리하지 않는다.

## 새 세션의 시작 순서

1. 저장소 루트에서 AGENTS → SESSION_STATE → 해당 backlog ID 순서로 읽는다.
2. 실제 체크아웃을 아래 명령으로 확인한다. 작업 중 변경을 지우거나 자동
   stash/pull하지 않는다. 원격 확인이 필요하면 먼저 안전하게 fetch하고,
   확인하지 못했으면 동기화를 `미확인`으로 기록한다.

   ```sh
   git rev-parse --show-toplevel
   git status --short --branch
   git log -3 --oneline
   git diff --stat
   git diff --cached --stat
   ```

3. 선택한 작업의 계획/체크리스트·관련 코드와 n−1/n−2를 읽는다. 전체 과거
   문서·그래프·로그를 다시 훑거나 완료된 기능을 다시 구현하지 않는다.
4. 상태 문서와 Git이 다르면 실제 변경과 PR/CI를 대조한다. `CI 대기`,
   `머지 완료·main 동기화 대기`, `동기화 완료`를 구분한다. 파일이나 로그가
   사라졌다는 이유만으로 과거 검증을 실패로 판정하지 않는다.
5. 다음 행동을 작업 ID와 함께 기록하고 진행한다. `다음 진행`은 명시된
   권장 작업과 허용 범위를 따른다. HOLD만 남았다면 그 조건을 설명하며,
   새 입력 없이 같은 디스크/RPC/호스트 검사를 반복하지 않는다.

AGENTS를 지원하지 않는 도구나 사람이 재개할 때도 SESSION_STATE가 같은
시작점이다. 새 채팅에 전체 대화 대신 다음 문장으로 재개할 수 있다.

> 저장소의 AGENTS.md와 docs/SESSION_STATE.md를 읽고 실제 Git 상태를 확인한 뒤,
> 명시된 다음 작업 ID를 이어서 진행해. 기존 운영 HOLD와 검증 경계를 유지해.

## 작업 상태와 완료 조건

상태는 backlog에서 정의한 `READY`, `PLAN`, `HOLD_INPUT`, `HOLD_RUN`,
`DEFERRED`, `DONE`을 사용한다. 기능 진행 단계는 별도로 기록한다.

| 단계 | 기록할 근거 |
| --- | --- |
| DESIGN | 선택한 ID·범위·의존성·완료 조건·계획/체크리스트 |
| RED | 실제 동작 요구가 실패한 명령/결과. 컴파일·캐시 접근 실패와 구분 |
| GREEN | 동일 요구의 통과와 새 경계/오류/취소 검사, 정확한 소스 식별 |
| REVIEW | 실제 수행한 fmt/build/vet/lint/race/문서/보안 검사와 미수행·skip 이유 |
| CI | PR URL·head SHA·CI run·현재 실제 결과. 로컬 통과로 CI를 대체하지 않음 |
| MERGED | squash commit·검토 tree·부모 및 PR 머지 결과 |
| MAIN_SYNCED | clean HEAD=origin/main, 설정 보존, 다음 ID와 보류 조건 |

문서 전용 변경에는 기능 RED나 runtime 검사를 만들어 붙이지 않는다.
코드 검사는 [기여 규칙](../CONTRIBUTING.md)과 [short/full CI](CI.md)를 따른다.
실제 시행한 기능 검사와 추가 성능 측정을 분리하고, 새 변경/실패 없이
SDK·DB·200회·호스트/SLO 검사를 반복하지 않는다.

## 작업 중단 시 최소 인계

진행 중 작업이 있으면 SESSION_STATE의 활성 작업 칸을 다음 항목으로 갱신한다.
빈 값은 임의로 채우지 않고 `없음`/`미확인`으로 구분한다.

```text
task_id / status / phase:
base / branch / tested_head_or_diff_digest:
scope / acceptance / plan / checklist:
changed_files / user_changes_to_preserve:
last_confirmed_check: command, source, outcome, skips
unresolved_failure: symptom, evidence, hypothesis, next_discriminating_action
PR / CI_run / last_observed_result:
running_job_owner: machine/session, command, lifecycle, optional temporary log
next_action / required_input / operating_hold:
previous_two_contexts:
```

실행 중 프로세스 정보는 관측 시점의 인계이며 이후 머신의 현재 상태를
보증하지 않는다. 다른 머신의 PID나 세션 ID로 종료/재개하지 말고 소유권을
먼저 확인한다. 사용자 변경과 미완성 코드를 완료 상태로 포장하지 않는다.
비밀 값·운영 데이터는 기록하지 않고 필요한 입력의 종류만 적는다.

다른 머신에는 마지막 push된 커밋만 전달된다. 로컬 미커밋 파일과 임시 로그의
경로를 적는 것만으로 파일 내용이 이동하지 않는다. 검토 가능한 작업을 허용된
브랜치/PR로 보존하되, 미완성 체크포인트는 그 상태와 미통과 검사를 명확히
표시하고 완료/머지로 간주하지 않는다. 공유하지 못한 변경은 `LOCAL_ONLY`로
적어 다른 머신이 같은 상태를 이미 가졌다고 추정하지 않게 한다. 비밀 파일은
인계 커밋에 넣지 않는다.

## 종료와 문서 갱신

- 작업/범위가 바뀔 때와 중단·종료 전에 현재 상태와 해당 backlog ID를 갱신한다.
  완료 항목은 활성 목록에서 빼고 요약·정본 링크로 남긴다. ID는 재사용하지 않는다.
- DESIGN·RED·GREEN·PR 생성·CI 결과 변경·머지·main 동기화가 인계 체크포인트다.
  결과가 변하지 않은 poll마다 상태 기록을 늘리거나 검사를 반복하지 않는다.
- n/n−1/n−2의 계획·검증·PR/CI 링크를 보존하고 새 단계로 이동할 때 회전한다.
  수용한 E1/first-import 설계는 단순한 과거 실험으로 취급해 삭제하지 않는다.
- 검증 명령·결과·source/head·skip·한계를 남긴다. 원시 로그/개인 경로는 부가
  자료이며 새 세션이 없으면 안 되는 유일한 근거로 만들지 않는다.
- 문서는 자기 자신의 최종 squash SHA를 미리 알 수 없다. 마지막 검증된
  기준점과 관련 PR을 기록하고, 현재 문서를 포함한 실제 HEAD/CI는 Git/PR에서
  대조한다. SHA를 채우기 위한 문서 커밋·CI 반복을 만들지 않는다.
- 다음 작업을 시작할 때 직전 릴리스의 실제 머지/CI 정보를 상태 문서에
  반영한다. 아직 진행 중인 CI나 미수행 운영 검증을 완료로 표시하지 않는다.

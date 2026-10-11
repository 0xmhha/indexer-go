# PostgreSQL undo 비용 줄이기 (설계, 10/11)

## 1. 왜

블록 쓰기를 batch로 보낸 뒤(#140) PostgreSQL ingest는 Pebble보다 2~4배 느리다(`BenchmarkIngest/regular`, 블록 202개, 거래 10,001건). 남은 시간의 대부분은 서버 쪽이다. `pg_stat_statements`로 보면 바뀐 행 하나마다 undo trigger(`undo_record`, migration 0006)가 돈다. 이 trigger는 다음 일을 한다.

- plpgsql 함수 호출
- 행 전체를 `to_jsonb(NEW)`로 바꾼다(거래·receipt의 `data` bytea가 hex 문자열이 된다)
- `jsonb_object_agg`로 primary key를 뽑는다
- `undo_log`에 행 하나를 INSERT한다(인덱스 2개)

commit 때는 그 행들에 높이를 쓰는 `UPDATE undo_log SET height`가 한 번 더 돈다. 이 시험에서 trigger는 80,624번 불렸다. 블록당 약 400번이다.

## 2. 무엇이 바뀌는가

시험에서 바뀐 행을 표마다 세면 다음과 같다(블록 202개).

| 표 | 바뀐 행 | 쓰는 방식 |
|---|---|---|
| address_transactions | 20,001 | INSERT만. 높이 열이 없다 |
| balance_history | 15,007 | INSERT만. `block_number` |
| balances | 15,007 | 계정마다 upsert(갱신) |
| transactions | 10,001 | 블록의 INSERT(같은 높이 재처리 때 덮어쓰기). `block_number` |
| receipts | 10,001 | 같음. `block_number` |
| logs | 5,000 | INSERT만. `block_number` |
| erc20_transfers | 5,000 | INSERT만(같은 키 덮어쓰기). `block_number` |
| blocks | 202 | 블록마다 하나. `number` |

높이 열이 있고 그 높이의 블록만 쓰는 표가 trigger 호출의 약 56%다. `address_transactions`에 높이 열을 더하면 약 81%가 된다. `balances`가 약 19%다.

## 3. 대안

### A. 쓴 블록의 높이로 지우는 표(권장)

블록 처리 중에 행을 넣기만 하는 표는 undo log가 필요 없다. 다른 블록이 그 행을 갱신하거나 지우지 않는다는 조건이다. 각 행에 "그 행을 쓴 블록 트랜잭션의 높이"를 적어 두면, 높이 h를 되돌리는 일은 `DELETE FROM 표 WHERE undo_height = h`와 같다.

- 열: `undo_height bigint DEFAULT nullif(current_setting('indexer.height', true), '')::bigint`와 그 인덱스. `SetHeight`가 블록 트랜잭션에 `indexer.height`를 정한다(C와 같은 값). 기본값으로 채우므로 쓰는 코드는 바뀌지 않는다. 블록 트랜잭션 밖에서 쓴 행은 NULL이고 rollback 대상이 아니다.
- 블록 번호 열(`block_number`)을 쓰지 않는 이유: 행의 블록 번호와 그 행을 쓴 트랜잭션의 높이가 다를 수 있다. genesis 잔액 조회(`assembly.go`)는 블록 h의 트랜잭션 안에서 블록 0의 `balance_history` 행을 쓴다. 블록 번호로 지우면 이 행이 rollback 뒤에 남고, rollback 뒤 다시 조회해 같은 행이 두 번 생긴다. 쓴 높이로 지우면 Pebble처럼 그 블록이 쓴 것만 정확히 지운다.
- 같은 높이를 다시 처리하는 경우(재처리, D3): 덮어쓴 행도 같은 높이이므로 지우는 것이 맞다. 거래가 다른 높이로 옮겨 가는 일(reorg)은 옛 높이를 먼저 되돌린 뒤에 일어난다.
- `undo_by_height`(새 표)에 이 방식의 표를 적고, `undo_block(h)`는 trigger 기록을 되돌린 다음 그 표들에서 `undo_height = h`인 행을 지운다.

대상(1단계): `blocks`, `transactions`, `receipts`, `logs`, `balance_history`, `erc20_transfers`, `erc721_transfers`, `address_transactions`.

넣지 않는 표: `internal_transactions`(다른 블록의 거래 행을 지운다. `TestRollbackRestoresEveryTable`이 그렇게 쓴다), 통계·잔액·소유·상태 표(갱신된다), `records`·`record_keys`(재구축이 지운다), DEX 표(주문은 갱신된다). 이 표들은 trigger를 그대로 둔다.

단점:
- 표가 "다른 블록의 행을 바꾸지 않는다"는 약속을 깨면 rollback이 틀린다. 그래서 시험으로 지킨다(5장).
- 열과 인덱스가 늘어 행마다 인덱스 갱신이 하나 더 든다(trigger보다 싸다고 본다 [Mid]).
- 기존 DB는 migration 전 행의 `undo_height`가 NULL이다. 그래서 migration 직후 128블록 창 안의 기존 블록은 이 표들을 되돌리지 못한다. 그래서 migration이 창 안의 기존 trigger 기록(`undo_log`)을 그대로 두어 그 블록들은 이전 방식으로 되돌린다(`undo_block`은 둘 다 처리한다).

### B. statement 단위 trigger(transition table)

`FOR EACH STATEMENT ... REFERENCING NEW TABLE`로 문장마다 한 번에 기록한다. 지금 쓰기는 문장 하나가 행 하나다(batch 안의 단일 행 INSERT). 그래서 호출 수가 줄지 않는다. 이득이 작아 권장하지 않는다.

### C. trigger 자체를 싸게(A와 함께)

- 높이를 쓸 때 기록한다. `SetHeight`가 `set_config('indexer.height', h, true)`를 대기열에 넣고, trigger가 그 값을 `height`에 바로 쓴다. 그러면 commit의 `UPDATE undo_log SET height`가 없어진다. 높이가 없는 트랜잭션은 이전처럼 commit 때 지운다.
- INSERT는 old가 없다. 그래서 `to_jsonb(NEW)` 대신 primary key 열만 뽑는다(`jsonb_build_object`를 표마다 만든 trigger 인자로). 효과는 A 뒤에 남는 표에만 해당한다.

### D. 잔액 upsert를 블록당 계정마다 한 번(A와 함께)

#140에서 잔액은 블록 트랜잭션 안에서 계정마다 한 번 읽고 메모리에 둔다. upsert도 바뀔 때마다 보내지 않는다. 대기열을 보낼 때(다른 문장 직전, commit 직전) 바뀐 계정만 한 번씩 보낸다. `balances`의 trigger 호출이 바뀐 횟수(15,007)에서 블록마다 바뀐 계정 수로 준다. 이력(`balance_history`)은 A로 trigger가 없다.

## 4. 권장

A + C(높이를 쓸 때 기록) + D. 예상: trigger 호출이 블록당 약 400번에서 수십 번(계정·통계·상태 표)으로 준다 [Mid: 측정 전 추정]. 같은 부하에서 번갈아 재서 확인한다.

## 5. 검증

- `TestRollbackRestoresEveryTable`(되돌린 뒤 모든 추적 표가 이전과 같다)을 높이로 지우는 표까지 넓힌다. 스냅샷 대상을 "trigger가 있는 표"에서 "trigger가 있거나 `undo_by_height`에 있는 표"로 바꾼다.
- `TestEveryTableIsUndoTracked`를 "모든 데이터 표는 trigger가 있거나 `undo_by_height`에 높이 열과 함께 있다"로 바꾼다.
- 새 시험: 높이로 지우는 표에서 블록 트랜잭션이 다른 높이가 쓴 행을 갱신하거나 지우지 않는지 확인한다. 시험 전용 trigger가 UPDATE·DELETE의 `OLD.undo_height`가 지금 높이와 다르면 오류를 낸다. 그 상태로 기본·StableNet·DEX·records 시나리오를 색인한다.
- `make test-postgres`의 reorg e2e(`TestLiveLoopRollsBackReorg` 계열, DEX reorg)와 `TestPostgresMatchesPebble`.
- `BenchmarkIngest`를 main과 번갈아 잰다.

## 6. 하지 않는 것

- Pebble 쪽은 바꾸지 않는다(undo가 batch 안의 키 단위 기록이라 이 비용이 없다).
- trigger 기반 undo를 없애지 않는다. 갱신되는 표는 계속 trigger로 되돌린다.

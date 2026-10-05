# Reorg 처리 상세 설계 (10/5)

이 문서는 refactoring-plan.md의 R2-4(finality·reorg)를 설계한다.

---

## 1. 결론

블록마다 되돌리기 기록(undo)을 남기고, 새 블록의 parent hash가 저장된 직전 블록과 맞지 않으면 갈라진 지점까지 되돌린 뒤 새 체인을 색인한다. undo는 블록 트랜잭션을 commit하기 직전에, batch에 든 키마다 DB의 이전 값을 읽어 같은 batch에 기록한다. 그래서 블록의 쓰기와 undo가 함께 commit되거나 함께 버려진다. undo는 최근 N블록(기본 128)만 보관한다. 그보다 깊은 reorg는 멈추고 재색인을 안내한다.

---

## 2. 지금의 문제

| 문제 | 결과 |
|---|---|
| parent hash를 확인하지 않는다 | 이미 색인한 블록이 체인에서 바뀌어도 그 위에 새 블록을 계속 쌓는다. 바뀐 블록의 거래·잔액·로그가 그대로 남는다 |
| 같은 높이의 hash가 다르면 `ErrBlockConflict`로 실패한다 | live 루프는 이 오류를 로그로 남기고 같은 높이를 끝없이 재시도한다. 색인이 멈춘다 |
| 되돌릴 방법이 없다 | 쓰기가 키 단위로 흩어져 있어, 블록 하나를 지우려면 모든 기능의 키 규칙을 알아야 한다 |

---

## 3. 설계

### 3.1 undo 기록

- `BlockTx.Commit` 직전에 batch의 연산을 처음부터 읽는다(`batch.Reader`). 처음 보는 키마다 DB(commit 전이므로 이전 상태)에서 값을 읽어 `{key, 있었는지, 이전 값}`을 모은다.
- 모은 목록을 `/undo/<높이 20자리>`에 같은 batch로 쓴다. 같은 batch에서 `/undo/<높이-N>`을 지운다(보관 창).
- `Set`과 `Delete`만 되돌릴 수 있다. 블록 처리 중에 범위 삭제가 쓰이면 그 블록은 되돌릴 수 없다고 기록한다. 지금 블록 처리 경로에는 범위 삭제가 없다.
- 높이는 블록 트랜잭션을 열 때 알려 준다(`BeginBlock` 뒤 `SetHeight`). 높이를 모르는 트랜잭션(backfill 등)은 undo를 쓰지 않는다.

비용: commit마다 블록이 쓴 키 수만큼 DB 읽기가 늘고, undo 크기만큼 쓰기가 늘어난다. 벤치마크로 측정한다.

### 3.2 되돌리기

`Rollback(to)`는 커서부터 `to+1`까지 높이를 내려가며, 높이마다 트랜잭션 하나로 undo를 거꾸로 적용한다. 이전 값이 있으면 다시 쓰고, 없었으면 지운다. 그리고 그 undo 기록을 지운다. 커서 키(`/meta/lh`)도 블록에서 쓴 키이므로 undo로 함께 돌아간다. 끝나면 메모리 캐시(주소별 순번, 거래 수, genesis 조회 기록)를 비워 다음 사용 때 디스크에서 다시 읽게 한다.

### 3.3 감지와 처리

- 블록 h를 색인하기 전에, 저장된 h-1의 hash와 h의 parent hash를 비교한다. 다르면 `ErrReorg{Height: h-1}`이다. 같은 높이를 다시 받았는데 hash가 다를 때(`ErrBlockConflict`)도 같은 뜻으로 다룬다.
- live 루프는 `ErrReorg`를 받으면 갈라진 지점을 찾는다. 저장된 높이 h-1부터 내려가며 노드의 같은 높이 블록 hash와 비교하고, 처음 같은 높이가 갈라진 지점 F다. F까지 되돌리고 F+1부터 다시 색인한다.
- F가 undo 보관 창 밖이면 `ErrReorgTooDeep`으로 멈춘다. 이 경우는 재색인해야 한다.
- 되돌린 블록마다 reorg 이벤트를 낸다(구독자가 화면을 고칠 수 있게). 이 이벤트는 3단계(G4)에서 넣는다.

### 3.4 finality 정책

StableNet(WBFT)은 블록이 즉시 확정되므로 reorg가 없다. 일반 EVM 체인은 reorg가 있다. 지연을 늘리지 않도록 기본은 head를 바로 색인하고 reorg를 되돌리는 방식으로 한다. 확정된 블록만 색인하는 정책(n confirmation, `finalized` 태그)은 설정으로 고를 수 있게 뒤 단계에서 넣는다.

---

## 4. 단계와 검증

| 단계 | 내용 | 검증 |
|---|---|---|
| G1 | undo 기록과 `Rollback` | 블록 k개를 색인한 DB를 j까지 되돌리면, 처음부터 j까지만 색인한 DB와 키·값이 같다(undo 키 제외) |
| G2 | parent hash 감지, 갈라진 지점 찾기, live 루프 처리 | 가짜 체인에서 reorg를 일으킨 뒤 수집이 이어지고, 결과가 바뀐 체인을 처음부터 색인한 DB와 같다 |
| G3 | 보관 창, 너무 깊은 reorg 거부 | 창보다 깊은 reorg에서 멈춘다 |
| G4 | reorg 이벤트, finality 정책 설정 | 구독 시험 |

---

## 5. 결정이 필요한 것

| 결정 | 선택지 | 권장 | 권장안의 단점 |
|---|---|---|---|
| undo 방식 | commit 때 이전 값 기록 / 블록을 다시 색인해 역연산 계산 | 이전 값 기록 | 블록마다 쓰기와 읽기가 늘어난다 |
| 보관 창 | 64 / 128 / 설정 | 128, 설정 가능 | 128블록보다 깊은 reorg는 재색인해야 한다 |
| 기본 정책 | head 색인 + 되돌리기 / n confirmation | head 색인 + 되돌리기 | 구독자가 되돌려진 데이터를 잠깐 볼 수 있다 |

---

## 6. 진행 기록 (10/5)

**결정.** 따로 지시가 없어 5절의 권장안(이전 값 기록, 보관 창 128, head 색인과 되돌리기)으로 진행했다.

### G1~G3

| 산출물 | 내용 |
|---|---|
| `pkg/storage/undo.go` | `BlockTx.SetHeight`, commit 직전 undo 기록(`/undo/<높이>`), 보관 창 밖 기록 삭제, `RollbackTo`(필요한 기록이 모두 있는지 먼저 확인한 뒤 높이마다 트랜잭션 하나로 되돌림), 되돌린 뒤 메모리 캐시 초기화, `DropUndo` |
| `pkg/fetch/index_block.go` | 블록 트랜잭션에 높이를 알린다. 저장된 블록과 hash가 다르면 `ErrBlockConflict`와 함께 `ReorgError`를 낸다. 새 블록의 parent hash가 저장된 직전 블록과 다르면 `ReorgError`를 낸다 |
| `pkg/fetch/reorg.go` | `HandleReorg`: 저장된 hash와 노드의 hash가 처음 같아지는 높이(갈라진 지점)를 찾아 되돌린다. undo 창보다 깊거나 undo가 없으면 `ErrReorgTooDeep`. reorg 수와 되돌린 블록 수를 지표에 남긴다 |
| live 루프 | `ReorgError`를 받으면 되돌리고 갈라진 지점 다음부터 이어 간다. `ErrReorgTooDeep`이면 멈춘다(예전에는 같은 높이를 끝없이 재시도했다) |
| backfill | backfill한 블록의 undo 기록을 지운다. 원래 undo에 backfill이 쓴 키가 없어 정확히 되돌릴 수 없기 때문이다. 그래서 그 블록까지 내려가는 reorg는 멈춘다 |
| 테스트 체인 | `Chain.Reorg(keep)`: keep 위의 블록을 버리고 상태(nonce, 잔액)를 되돌린다 |

**keyspace 비교에서 undo 제외.** undo 기록은 체인만이 아니라 처리 이력(재시작, backfill)에 따라 달라진다. 그래서 keyspace golden과 결과 비교에서는 `/undo/`를 빼고, undo는 아래 되돌리기 시험으로 검증한다.

검증 결과는 다음과 같다.
- `TestRollbackRestoresEarlierState`: 끝까지 색인한 DB를 높이 0, 3, 10, 19로 되돌리면, 그 높이까지만 색인한 DB와 키·값이 같다. 이어서 나머지를 다시 색인하면 golden과 같다(메모리 캐시 초기화 확인).
- `TestRollbackWithoutUndoFails`: 중간 높이의 undo가 없으면 `ErrNoUndo`로 실패하고 아무것도 되돌리지 않는다.
- `TestLiveLoopRollsBackReorg`(프로필 경로, 기존 클라이언트 경로): 색인한 체인의 마지막 3블록을 다른 분기 5블록으로 바꾸고 live 루프를 돌리면, reorg 1번에 3블록을 되돌리고 새 분기를 색인한다. 결과는 새 체인을 처음부터 색인한 DB와 같다.
- `TestReorgBeyondUndoStops`: 되돌릴 구간에 undo가 없으면 live 루프가 `ErrReorgTooDeep`으로 멈추고, 커서와 데이터는 그대로다.
- 전체 시험과 live StableNet 시험(블록 0~3000)이 통과했다.

**비용.** `BenchmarkIngest` 원자 경로의 일반 블록이 2.83~3.07초로, 직전 측정(2.55~2.73초)보다 약 10% 늘었다. 대형 블록은 3.0~3.27초로 직전(3.2~3.7초)과 비슷하다. 측정 잡음이 커서 정확한 비율은 아니다.

**남긴 것.**
- G4: reorg 이벤트(되돌린 블록을 구독자에게 알림)와 확정 블록만 색인하는 정책(n confirmation, `finalized` 태그).
- 기존 비원자 경로(`atomic_block: false`)는 undo를 쓰지 않으므로 reorg를 감지하지도 되돌리지도 않는다. 이 경로는 S5에서 지운다.
- gap 복구 경로는 `ReorgError`를 받으면 오류로 끝난다. live 루프만 되돌린다.

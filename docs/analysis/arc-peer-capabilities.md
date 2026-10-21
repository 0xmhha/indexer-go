# Arc peer 공급·검증 감사 (2026-10-15)

## 확인한 소스와 선택한 경로

공식 Arc `6e764023ee6515fe70573e123ed2db912a7207b4`의 [운영 가이드](https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/docs/running-an-arc-node.md)는 실행 계층(EL)과 합의 계층(CL)을 분리한다. [workspace](https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/Cargo.toml)의 EL은 upstream Reth v2.2.0 SDK(`88505c7fcbfdebfd3b56d88c86b62e950043c6c4`)이고 CL은 Malachite/libp2p다. 따라서 실행 history와 합의 certificate를 같은 wire 데이터로 가정하지 않는다.

고정 Reth의 [EthVersion](https://github.com/paradigmxyz/reth/blob/88505c7fcbfdebfd3b56d88c86b62e950043c6c4/crates/net/eth-wire-types/src/version.rs)에서 협상 목록에69/68/67/66이 있음을 확인했다. 첫 adapter는 기존 Go geth의 RLPx와 **ETH/68** header/body/receipt 요청을 사용한다. [프로토콜 문서](https://github.com/ethereum/devp2p/blob/master/caps/eth.md)의 최신 버전과 wire layout을 혼용하지 않는다. 특히 ETH/69 이후의 status/receipt layout을68로 디코딩하지 않는다.

실제 연결은 공식 온보딩으로 확인한 EL enode·network/fork ID·genesis/checkpoint가 필요하다. 소스의 프로토콜 지원이 공개 Arc peer 접속이나 데이터 보존을 보장하지 않는다. 현재 확인은 공식 소스, 기록 Arc 실행 입력과 독립 loopback ETH/68 oracle에 한정한다. 실제 네트워크 peer 주소가 없어 live 공급·보존 범위·전파 간격은 미검증이다. CL 직접 gossip/certificate 수신은 이번 구현에 포함하지 않는다.

## 자료별 공급·검증과 plan

| 자료 | 형식과 공급 | 현재 검증·보존 범위 | plan 상태 |
| --- | --- | --- | --- |
| block header | ETH/68 header RLP | profile hash/높이, parent 연속성, fresh canonical hash. stateRoot는 commitment만 보존 | `headers/complete` |
| block body | ETH/68 body RLP: transactions, uncles, optional withdrawals | 거래 trie/uncle/withdrawal root·거래 디코딩/서명자, exact RLP | `bodies/complete` |
| transaction | body 안의 legacy/typed transaction | body commitment와 chain profile로 해석. 정규화 transaction table은 후속 | body 안에 보존, 별도 dataset 미지원 |
| receipt | ETH/68 consensus receipt RLP | receipt root, 거래 수/유형, cumulative gas, header bloom. RPC 파생 메타데이터는 별도 자료 | `receipts/complete` |
| log | receipt 안의 address/topics/data | receipt commitment로 결합, 개수/토픽 상한. 별도 log 검색 table은 후속 | receipt 안에 보존, 별도 dataset 미지원 |
| account/contract/code/storage 변화 | 표준 header/body/receipt에는 전체 변경분이 없음 | stateRoot만으로 실제 상태값/전체 변경목록을 복원할 수 없음. witness/검증된 relay 또는 실행이 추가로 필요 | complete/tracked 모두 미지원 |
| state input | 상태 proof/witness/실행 입력이 별도로 필요 | ETH history에 존재한다고 가정하지 않음. 공개 RPC의 임의 과거 state 보존도 보장되지 않음 | 미지원 |
| trace/internal call | 실행 trace 또는 별도 RPC 공급 | 선택 call_traces는 hash-selected Geth 형식·최상위 거래/receipt 대조·provider 보고 범위로 보존한다. 하위 실행/누락은 자체 증명하지 않음. 실제 Arc debug 지원은 확인 필요 | base plan unsupported, [선택 RPC](../CALL_TRACES.md) 구현 |
| blob | 거래의 versioned hash와 실제 sidecar는 별도 | 고정 공식 txpool은 EIP-4844 admission을 비활성화한다. Arc profile의 supplier/조회 guard와 durable 상태 audit를 구현. 과거 모든 블록의 부재 증명은 아님. generic 전체 blob/KZG library는 재사용 가능; cell/sample은 별도 | Arc node policy unsupported, [근거·조회 계약](../BLOB_INPUTS.md#arc-정책과-상태-조회) |
| consensus finality | CL Malachite block/certificate, 명시적 RPC finalized 신뢰 모드 | ETH header hash와 fresh RPC 일치가 CL quorum 검증은 아님. `RunManaged`는 감사한 Arc Engine forkchoice와 선택한 동일 RPC의 finalized prefix를 신뢰하고 authority를 DB에 고정 | trusted RPC 정책/선택 GC 연결, 독립 certificate 검증 미구현 |

`evmdata.BasePlan()`은3개의 complete dataset만 요구한다. `tracked` 주소를 공급한다고 선언하거나 부족한 required를 빼서 시작하지 않는다. 다른 required/codec/chain을 요구하면 fail closed한다. 빈 블록은 실제 empty-list RLP의 root/gas/bloom 검증으로 인정하며, peer의 빈 응답/누락과 구분한다. 이 plan은 포괄 Web3 상태/trace 저장의 완료가 아니다.

`VerificationCommitment`는 입력이 fresh canonical header의 root/hash에 결합됨을 뜻한다. EVM 재실행·상태 전이·모든 chain fork consensus rules·CL finality를 자체 검증했다는 뜻은 아니다. canonical 신뢰 경계는 기존 live RPC reader이며 다른 fork/누락을 만나면 capture 높이를 전진시키지 않는다.

## 통신·큐·RPC 예산

- 명시적 static peer 최대8개, 기본4개. discovery crawl/txpool은 없다. status는 network/genesis와 명시적 fork ID의 **완전 일치**를 요구한다. fork 전환 때 호환 ID/정책 갱신이 필요하다.
- 최초 remote head hash→header 한 번으로 advertised 높이를 확인하고, 이후 한 블록당 header/body/receipt3회 P2P 요청을 순서대로 보낸다. 다음 높이가 없으면 기본100ms 대기한다. 알려진 head 아래 자료 누락은 `ErrHistoryGap`이며 자동 skip하지 않는다. transport 단절은 같은 높이로 재연결한다.
- 메시지10MiB, header64KiB, dataset8MiB, 거래/receipt/총 로그32,768개, 로그 토픽4개로 제한한다. 애플리케이션 RLP cardinality를 decoder 할당 전에 확인한다. 모든 수신 byte는 bounded envelope와 window quota를 거친다.
- 수신은 fsync 뒤 bounded 참조 channel에 알리고, 검증 worker는 durable raw Page를 읽는다. channel 기본256개가 가득 차도 작업은 디스크에 남는다. 메모리의 무제한 블록 queue로24시간 window를 대체하지 않는다. callback은 즉시 반환해야 한다.
- body/receipt 수집용 HTTP는 추가하지 않는다. 시작의 genesis/checkpoint 확인과 매 capture commit 직전 fresh canonical 조회는 caller의 **같은5 HTTP/s pool**을 쓴다. 기존 writer/projection의 조회도 이 예산을 공유해야 한다. fresh canonical은 batch coalescing/cache로 생략하지 않는다. 초기 확인에는 복수 조회가 들며 정상 연속 실행의 capture commit에는 블록당 최소1회가 든다. 보충 state/trace RPC는 현재 plan에 없고 추후 필요하면 물리 HTTP와 provider의 논리 method quota를 별도로 계산한다.
- 검증+DB commit 이후에만 최근1024블록(최대8192)의 참조를 peer 응답용 메모리에 게시한다. 원본 바이트는 window lease로 읽는다. 초기 checkpoint 자체나 재시작 전 모든 보유 history를 서비스한다고 선언하지 않는다.

## 구현·시험과 남은 증거

[Arc 모듈](../../x/arc/feed/README.md), `pkg/chainfeed/ethpeer`, `pkg/chainfeed/evmdata`, `pkg/capture.PipelineTasks`를 연결했다. loopback 실제 TCP/RLPx, Pebble/PostgreSQL, 처리기 정지 중 수신·부분 재시작·손상/누락·fresh mismatch·기존 제한 HTTP pool, 기록 블록67,019,625를 검증했다. 시험 결과는 [작업 기록3.40](work-log-2026-10.md#340-arc-eth68-peer와-두-worker-연결-1015)에 남긴다.

실제 Arc peer의 ETH/68 활성화·허용 연결·fork ID·history 보존·네트워크 단절/회복은 추가 검증이 필요하다. 현재는 ETH history polling이며 직접 gossip의 빠른 전파를 측정한 구현이 아니다. [Feed 런타임](../FEED_RUNTIME.md)은 명시적 RPC-finalized 정책·bounded reorg 재개·raw ACK·선택적 GC를 라이브러리로 연결한다. 독립 CL certificate 검증·이전 fork 원본 삭제 정책,450ms/2블록초·실제 peer/장기 운영, 전체 상태/trace/blob 공급, 기존 writer의 DB-only 처리와 앱/네 체인 조립은 후속 목록을 따른다. full node/노드 DB/snapshot은 설치하거나 실행하지 않았다.

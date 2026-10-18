# 전체 blob 공급·검증 모듈

`pkg/chainfeed/evmblob`은 별도 공급자가 전달한 **전체 EIP-4844 blob**을 검증하고 영구 capture DB에 저장하는 라이브러리다. 독립 순차 worker와 DB-only 페이지를 제공한다. 후속 감사에서 고정 Arc 공식 노드의 blob 거래 비활성화 정책을 확인했다. Arc 공급기는 시작하지 않으며 현재 `source.feed` 설정에 blob 필드를 추가하거나 운영 config.yaml을 바꾸지 않았다.

## Arc 정책과 상태 조회

공식 Arc `6e764023ee6515fe70573e123ed2db912a7207b4`의 [생산 txpool 구성](https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/crates/execution-txpool/src/pool.rs)은 `.no_eip4844()`와 blob cache0을 사용하고, 같은 파일의 시험도 blob 거래 거부를 확인한다. 이 근거는 **해당 노드 구성의 transaction admission 정책**이다. 모든 과거 블록의 blob 부재나 모든 consensus/fork 규칙을 증명한 것으로 확대하지 않는다. 실제 public deployment의 실행 버전도 별도 운영 증거가 필요하다.

정책은 `pkg/chains/arc`에 두고 optional `chains.BlobCapabilityProfile`로 전달한다. generic EVM·optional 인터페이스가 없는 외부 profile은 `unknown/not_audited`다. embedding project profile이 ID를 바꾸면 원래 audit를 상속하되 선택된 외부 ID에 metadata를 결합한다. getter는 값 복사본을 반환한다.

`evmblob.New`와 profile을 사용하는 DB-only `ReadSnapshot`/`ReadPage`는 `unsupported`이면 `evmblob.ErrUnsupported`로 중단한다. provider/DB/canonical RPC를 호출하지 않으며 ‘검증된 빈 결과’로 바꾸지 않는다. 순수 `Decode`/`Verify`는 표준 암호 검증 도구로 유지한다. `unknown`인 profile의 library 사용은 명시적 provider와 실제 captured 입력을 전제로 하며, 앱에서 공급이 가능하다고 선언하는 것은 아니다.

Arc module은 code-owned audit를 등록한다. invalid metadata는 순수 설정 검사에서 거부하고, module Open 결과의 실제 profile 정책과 등록 audit가 다르면 조립을 거부한다. `blobs` YAML 활성화는 여전히 strict unknown-field 오류다. 검증 library만 있는 상태에서 실행 가능한 transport 설정으로 받아들이지 않는다.

앱은 audit를 기존 durable projection descriptor에 함께 기록한다. `GET /indexing/status`의 `source.blobCapability`는 다음 정보를 DB-only로 반환한다.

```json
{
  "version": 1,
  "profile": "arc",
  "status": "unsupported",
  "reason": "eip4844_disabled_by_node_policy",
  "basis": "node_transaction_admission",
  "revision": "6e764023ee6515fe70573e123ed2db912a7207b4",
  "sourceUrl": "https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/crates/execution-txpool/src/pool.rs"
}
```

이는 공급 정책 metadata이며 기존 `datasets[].coverage`와 독립적이다. blobs의 required plan/완료 cursor를 만들지 않는다. 기존 descriptor에 이 필드가 없으면 `null`로 유지하고 chain ID/header0/current registry를 근거로 소급 추론하지 않는다. 새 정보는 ingest의 정상 descriptor 기록 때 반영한다. unpinned URL·비밀값/쿼리·잘못된 revision/상태·unsupported인데 blobs required인 모순을 거부한다. 손상 metadata는 상태 HTTP500이며 잘못된 요청이나 빈 결과로 표시하지 않는다. 읽기는 기존 두 snapshot 경계를 유지하고 RPC/write를 추가하지 않는다.

## 검증 범위

먼저 영구 header/body/receipts를 chain profile과 commitment로 다시 검증하고, 서명된 type-3 거래의 `blob_versioned_hashes`를 거래/해시 순서로 펼친다. 각 위치마다 다음을 확인한다.

- 응답의 chain ID·genesis·블록 높이·hash가 exact captured input과 같다.
- 모든 참조에 대응하는 전체 blob이 정확히 하나씩 있다. index는0부터 연속이며 누락·추가·순서 변경을 거부한다. 같은 versioned hash를 여러 위치에서 사용하는 것은 허용한다.
- blob은131,072바이트, commitment/proof는 각각48바이트다. versioned hash는 `0x01 || sha256(commitment)[1:]`와 같다.
- 기존 go-ethereum v1.16.5의 `kzg4844.VerifyBlobProof`가 blob과 commitment/proof를 검증한다. 잘못된 곡선점·비정규 field element도 거부한다. 새 의존성이나 신뢰 설정 다운로드는 없다.
- header의 blob gas와 참조 수가 일치한다. type-3 거래가 있는데 필요한 header 필드가 없거나 해시 버전이 다르면 중단한다.

근거는 [EIP-4844의 거래·네트워크 계약](https://eips.ethereum.org/EIPS/eip-4844)이다. ETH `BlockBodies`의 거래 payload는 전체 blob을 포함하지 않는다. [EIP-7594](https://eips.ethereum.org/EIPS/eip-7594)의 cell/sample·다중 cell proof를 이 형식으로 받거나 전체 blob 완료로 인정하지 않는다.

검증 표시는 `kzg_full_blob_and_captured_transaction`, 범위는 `all_eip4844_blob_references`다. execution 재실행·fork별 최대 blob 수/수수료 규칙·beacon 서명/SSZ inclusion·독립 finality/네트워크 전체 data availability 검증을 뜻하지 않는다. canonical 신뢰는 기존 fresh reader에 있다. Arc header에 blob 관련0 필드가 있다는 사실만으로 Arc의 blob 지원을 선언하지 않는다.

본문에 type-3 참조가 없고 header gas도 일치하면 검증된 빈 결과를 저장한다. 공급자를 호출하지 않으며, ‘이 블록에 EIP-4844 참조가 없음’을 의미한다. 참조가 있는데 공급이 실패/만료/누락된 경우와 구분한다.

## 공급 계약과 후처리

`Provider.Open(ctx, FeedBlock, []Reference)`는 체인별 peer/beacon/relay 모듈이 구현한다. 이 라이브러리는 기본 endpoint·Engine JWT·가짜 Arc RPC method를 만들지 않는다. `engine_getBlobs`나 txpool의 보존을 영구 history 공급으로 가정하지 않는다. 새 HTTP adapter는 canonical reader와 **동일한5 HTTP/s pool**을 빌려 모든 실패/재시도를 제한해야 한다. 실제 공급 경로를 정할 때 별도 논리 method quota도 확인한다.

반환값은 `io.ReadCloser`이며 context를 따라야 한다. Close는 Read와 동시에 안전하고 대기 중인 Read를 풀어야 한다. 모듈은15초 요청 context, 바이트 제한, 취소 시 Close/callback join을 적용한다. 한 번에 전체 블록32개 blob까지만 처리한다. 이것은 운영 메모리/저장 한계이며 chain consensus 최대치가 아니다. 더 큰 블록은 실패하고 새 버전의 계약이 필요하다.

wire codec은 `eth.full-blobs.rlp.v1`, 정의 format은 `eip4844.full-blob-proof.v1`이다. 모듈의 relay RLP는 아래와 같다. Ethereum beacon BlobSidecar SSZ나 pooled transaction 자체의 wire format은 아니다.

```text
Response = [1, [[chainID_decimal, genesisHash], height, blockHash], entries]
Entry    = [blockWideIndex, blob_131072_bytes, commitment_48_bytes, proof_48_bytes]
```

RLP bytes/cardinality를 배열 할당 전에 확인하고 고정 길이·필드 수·정규 integer·trailing bytes를 검사한다. 응답 한도는 `MaxResponseBytes`(약4.01MiB), 영구 supplement는8MiB 이하, 페이지는 최대4개 blob이다. cell proof version/추가 필드는 거부한다. 공급자에게 넘기는 참조 배열은 별도 복사본이다.

프로젝트 조립 순서:

1. `Definition{Provider: <고정 label>, Format: evmblob.Format, Profile: <실제 profile ID>}`와 `Descriptor(name, fromHeight, definition)`을 만든다. label은 URL/비밀값이 아니다.
2. 기존 capture store의 `InitializeSupplementStream`으로 exact name/정의/시작 높이를 고정한다. 시작은 anchor 다음부터다. `required`/`parent_checkpoint`는 지원하지 않는다.
3. 같은 chain/profile/store와 실제 provider로 `evmblob.New`를 만들고, owner task group에서 `Run(ctx, descriptor, poll)`을 실행한다. poll 기본50ms, 허용20ms~5초다. 단일 블록 수동 공급은 `Acquire`를 사용한다.
4. owner는 worker 실패 시 연관 task를 취소/join하고, 종료 시 worker가 끝난 뒤 빌린 DB/client를 닫는다. 라이브러리는 DB·provider의 소유권을 가져가지 않는다.

captured 연속 prefix가 디스크 기반 작업 큐다. 한 블록만 active이며 누락 자료를 skip하지 않는다. reset 후 exact canonical prefix만 재사용하고 마지막 완료 자료를 다시 검증한다. fresh mismatch에서는 기존 capture supervisor의 epoch reset을 기다리며 임의 epoch를 만들지 않는다.

실제 transport의 수신 시점/sidecar retention은 별도 계약이다. 원격 자료가 사라지기 전에 durable하게 확보하는 head 수신 경로는 아직 연결하지 않았다. 로컬24시간 raw window가 원격 blob24시간을 보장하지 않는다. 실제 공급 모듈을 연결할 때 초기 수신/후처리 분리와 디스크 window에 보관할 형식·용량을 함께 검증해야 한다.

## 저장·조회·삭제 경계

검증 후 version/identity/definition/원응답을 RLP bundle로 저장한다. 기존 `CommitStreamSupplement`가 input·receipt·연속 cursor·자체 event의4개 write를 한 durable transaction과 **uncached fresh canonical1회**로 commit한다. 현재 head의 같은 입력 재시도는 공급자를 다시 열지 않고 bytes/KZG를 재검증하며 fresh는 다시 실행한다. 과거 완료 입력 조회는 `ReadSnapshot`/`ReadPage`를 사용한다.

기본 header/body/receipt plan·Through·raw ACK·GC 권한·기존 feature/global undo/outbox는 바뀌지 않는다. 이 선택 stream이 완료돼도 미처리 원본을 지우거나 필수 capture plan에 blobs를 추가할 수 없다. 최소24시간/필수 proof/finality/ACK/lease gate를 유지한다. 저장은 기존 capture namespace와 bound SQL 값을 사용하며 새 SQL/table을 만들지 않는다.

`ReadSnapshot`/`ReadPage`는 `capture.NewReader`의 읽기 전용 facade로 사용할 수 있다. 매번 source commitments·KZG·현재 epoch를 재검증하므로 DB digest를 함께 조작해도 잘못된 blob을 신뢰하지 않는다. 원본 source facts가 외부 객체라면 명시적으로 동일한 영구 object area를 전달한다. 누락 시 raw/RPC/내부 디스크 fallback은 없다.

`facts.Source.Blobs`도 같은 DB-only 페이지를 제공한다. 매 페이지는 exact epoch·CaptureIdentity·selection에 고정해야 한다. Source는 생성 epoch에 고정되며 reset 후 stale 오류를 반환한다. 반환값은 identity/selection/검증 범위·전체 수·최대4개 blob·next ordinal이다. HTTP route/config/status 등록은 실제 공급 adapter를 고른 뒤 별도로 연결한다. 대량 동시 proof 검증의 admission도 그 API에서 제한해야 한다.

`feedtelemetry` recorder를 context로 전달하면 `blobAcquire`, `blobSupply`, `blobVerify`, `blobCommit`의 bounded 실행 표본을 기록한다. 계측 자체는 DB/RPC를 쓰지 않는다. 앱에 blob worker를 자동 등록하지 않으므로 기존 앱에서 이 항목의 표본은 없다.

## 검증과 남은 작업

독립적인 zero/constant-one 다항식 벡터를 사용했다. commitment는 각각 identity/표준 BLS12-381 G1 generator, quotient proof는 둘 다 identity다. production 검증기의 commitment/proof 생성 함수로 기대값을 만들지 않았다. 서명된 거래와 header/body/receipt는 별도로 구성했다. 암호 라이브러리 전체 conformance 시험을 대체하는 것은 아니다.

macOS race, PostgreSQL16 fsync-on/단일 연결, Linux와 CPU1/race/count200을 검증했다. 공급 실패·증명/범위/길이 오류·4 write rollback·fresh mismatch·재시작/재시도·reorg·digest를 다시 계산한 DB 변조·DB-only 조회·취소/join·공유5 HTTP/s/failover가 대상이다. 상세 근거는 [작업 기록3.52](analysis/work-log-2026-10.md#352-전체-blob-검증공급-worker와-영구-db-조회-1016)에 남긴다.

다음은 추가 source 검색/색인이다. 전체 blob transport/보존/앱 연결은 이를 지원하는 체인과 실제 공급 경로를 정한 뒤 진행하며 Arc용 활성화 작업으로 자동 반복하지 않는다. full node나 snapshot을 다시 설치하지 않는다. 실제 Arc 처리량·네 테스트넷 장기 운영을 이 로컬 시험으로 완료 처리하지 않는다.

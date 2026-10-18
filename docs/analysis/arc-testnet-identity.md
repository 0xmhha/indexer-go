# Arc 테스트넷 genesis·network·fork 확인 (2026-10-19)

[운영 초안](../OPERATIONS_DRAFT.md)의 입력값 확인 기록이다. 공개 RPC를 읽었으며 노드/indexer·DB·Docker는 실행하지 않았다. 디스크 포맷·서비스 실행·성능 측정은 보류 상태다.

## 확정 값과 계산 후보

RPC는 기존 `https://rpc.testnet.arc.io`를 사용했다. identity 조회는 **2026-10-19 03:18:54 KST**에 시작했고, fork 계산용 반환 head는 높이 **67,881,454**, timestamp **1792347536**(2026-10-19 03:18:56 KST)이다. 이 head는 조회 시점의 관측값이며 finalized checkpoint로 선택하지 않았다.

| 항목 | 값 | 확인 수준과 초안 반영 |
| --- | --- | --- |
| chain ID | 5042002 (`0x4cef52`) | `eth_chainId` 응답·공식 genesis config 일치; 기존 값 유지 |
| network ID | **5042002** | `net_version`의 문자열 응답을 정수로 확인; YAML placeholder 교체 |
| genesis hash | **`0xe20e653af4441e8c6088e172b129d56420139824400477287b46e7101ae2bb1f`** | block0 RPC hash·공식 chainspec 테스트 상수 일치; YAML placeholder 교체 |
| genesis timestamp | 1757515087 (`0x68c18d4f`) | 공식 genesis JSON·block0 RPC 일치 |
| 예상 fork hash | **`910609e1`** | 아래 공개 소스 일정과 관측 head로 계산; 실제 peer 광고값 미확인 |
| 예상 fork next | **0** | 같은 고정 소스의 알려진 일정이 관측 head에서 모두 활성화됨; 네트워크의 모든 향후 변경 부재를 뜻하지 않음 |

`network_id`를 chain ID에서 추정하지 않고 `net_version`으로 별도 확인했다. ETH/68 peer status의 network/genesis/fork를 실제 연결로 대조한 것은 아니다. **fork hash/next는 YAML에서 `UNCONFIRMED_*`를 유지**한다.

## 공식 소스와 RPC 배포 버전의 차이

확인 당시 공식 저장소 main은 기존 감사 commit **`6e764023ee6515fe70573e123ed2db912a7207b4`**와 같았다. 확인한 파일은 다음과 같다.

- [공식 testnet genesis](https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/assets/testnet/genesis.json): chain ID·genesis 입력.
- [공식 chainspec](https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/crates/execution-config/src/chainspec.rs#L570): `arc-testnet`이 JSON을 읽은 뒤 `ARC_TESTNET_HARDFORKS`로 일정을 교체함. `test_arc_testnet_chainspec`의 genesis hash 상수도 대조함.
- [공식 hardfork 일정](https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/crates/execution-config/src/hardforks.rs#L329): Arc 전용 일정 포함.
- [고정 Reth의 fork 계산](https://github.com/paradigmxyz/reth/blob/88505c7fcbfdebfd3b56d88c86b62e950043c6c4/crates/chainspec/src/spec.rs#L661): block 먼저, genesis 이후 timestamp 다음, 같은 활성화 지점 중복 제외. [EIP-2124](https://eips.ethereum.org/EIPS/eip-2124)·[EIP-6122](https://eips.ethereum.org/EIPS/eip-6122)를 따름.

별도 버전 조회에서 `web3_clientVersion`은 `arc/v1`, `arc_getVersion`은 다음을 반환했다.

```text
git_version: v0.8.1-hotfix.rc1
git_commit: 6ede3f18a211315078909e9d3b5be1f6608fe9e8
```

이 배포 commit을 사용한 공식 raw GitHub 경로의 `chainspec.rs`, `hardforks.rs`, `assets/testnet/genesis.json`은 모두 HTTP404였다. 소스가 공개된 감사 commit과 배포 commit이 동일하다고 간주할 수 없고,404만으로 변경 내용이나 원인을 알 수 없다. RPC가 fork ID 자체를 반환한 것도 아니다. 따라서 계산 후보를 실제 peer의 검증된 설정값으로 승격하지 않았다.

실제 enode를 확보한 후 해당 peer의 network/genesis/fork 광고값과 소프트웨어 일정을 대조해야 한다. 현재 adapter는 fork ID 완전 일치를 요구하므로 후보를 맞추기 위해 검사 조건을 완화하지 않는다. 공개 [운영 가이드](https://github.com/circlefin/arc-node/blob/6e764023ee6515fe70573e123ed2db912a7207b4/docs/running-an-arc-node.md#peering)는 온보딩에서 받은 EL enode를 사용한다. 이번 확인에서 enode를 추측하거나 P2P 접속을 시도하지 않았다.

## fork 계산 근거

genesis JSON의 Ethereum fork들은 genesis부터 활성화돼 있지만, built-in testnet은 다음 추가 일정을 사용한다. **genesis JSON만 읽으면 Arc 전용 fork를 누락한다.**

| fork | 기준 | 활성화 값 |
| --- | --- | ---: |
| Zero3 | 블록 높이 | 11172019 |
| Zero4 | 블록 높이 | 26148086 |
| Osaka | Unix timestamp | 1779890400 |
| Zero5 | Unix timestamp | 1779894517 |
| Zero6 | Unix timestamp | 1779894517 |
| Zero7 | Unix timestamp | 1781791200 |
| Zero8 | Unix timestamp | 1788447600 |

관측 head는 모든 값을 지났다. genesis 활성화 항목은 추가하지 않고, Zero5·Zero6의 같은 timestamp는 **한 번만** 넣는다. 입력은 genesis hash의 실제32바이트 다음에 각 숫자의8바이트 big-endian 표현을 이어 붙인 것이다. hex 문자열이나 chain ID를 CRC 입력으로 사용하지 않는다.

| CRC32 입력 누적 | fork hash |
| --- | --- |
| genesis hash | `03113c69` |
| + block11172019 | `2b0ec94f` |
| + block26148086 | `ffc10262` |
| + time1779890400 | `40fc2273` |
| + time1779894517 | `6d80ce8d` |
| + time1781791200 | `59eef8b2` |
| + time1788447600 | **`910609e1`** |

Python 표준 CRC32와 별도 bitwise IEEE CRC32 계산 결과가 일치했다. EIP의 Ethereum genesis/Homestead/DAO·timestamp 예시 벡터도 대조했다. Rust/노드 소스를 빌드하거나 실행한 결과가 아니며 peer handshake 완료 증거도 아니다.

## 조회 범위와 남은 값

RPC는 identity4개 method의 HTTP 배치1회와 버전2개 method의 배치1회로 **총2회**였다. 전체 HTTP 요청 자체가2회이므로 초당5회 한도를 넘지 않았고 재시도나 우회 endpoint도 사용하지 않았다. 속도·지연·처리량을 측정하지 않았고 archive/snapshot·노드 저장소를 다운로드하지 않았다. 공식 소스 텍스트와 RPC 원문·파생 계산 기록은 `/private/tmp/indexer-arc-identity-20261019/`에 보존했다.

APFS 확보/포맷·DB 접속·실제 enode·peer fork ID·finalized anchor/hash·시작 높이·identity 생성·초기화/GC 활성화는 계속 보류/미확정이다. [YAML 초안](../examples/config-testnets-windowed.draft.yaml)은 숫자 placeholder가 남아 그대로 실행할 수 없다. 체인별 PostgreSQL pool4개(합계16), 공유 RPC5 HTTP/s, source와 writer 커밋 직전 fresh·epoch·내구성/ACK/삭제 fence와 다른 프로젝트를 유지한다.

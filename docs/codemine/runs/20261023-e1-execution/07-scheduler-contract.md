# U05: bounded execution scheduler

현재 U04~U08 통합 브랜치에서 기능 검사 완료. 공개 transaction과 실제 앱 연결 완료를 의미하지 않는다.

- `Prepare`는 wire를 소유한 자료로 복호화하고 기존 chain profile/evmdata 검증기로 header·body·receipt commitment, 거래 순서와 journal metadata를 대조한다. EVM·RPC는 실행하지 않는다. 명시적 anchor baseline만 허용하며 일반 결과의 baseline 단계는 거절한다.
- sender와 성공한 outer CREATE receipt가 일치할 때만 creation 증거를 만든다. 빈 runtime code도 포함하며 실패 receipt는 제외한다. 내부 CREATE의 creator를 추정하지 않는다.
- cardinality/byte 제한은 profile allocation 전에 적용한다. 기존 capture 기본 제한은 유지한다. wire encoder도 decoder의 JSON token budget을 넘는 자료를 거절한다.
- scheduler는 intake 1개, 표별 queue 7개, 입력 순서의 publication queue를 소유한다. 큐들은 같은 불변 Prepared를 참조한다. 총 블록·보수적 메모리 charge는 active 작업부터 publication 완료까지 포함한다.
- 표별 worker는 순차 처리하고 FIFO semaphore의 permit 2개를 공유한다. permit은 자기 DB transaction에만 사용한다. queue·다른 writer·publication·RPC 대기 중에는 보유하지 않는다. 기존 pool4를 늘리지 않는다.
- Submit 성공은 블록 전체의 소유권 이전이다. 호출자의 Future.Wait 취소가 특정 queue의 높이를 누락시키지 않는다. 처리 중 exact duplicate는 기존 Future를 공유하고 추가 예산을 점유하지 않으며 다른 digest는 거절한다. 치명 오류/종료는 전체 수신·분배·worker·publication을 취소하고 join한 뒤 미완료 Future를 실패시킨다.
- StoreBackend는 WriteExecutionPart의 bound context를 core4/state3 writer에 전달한다. 일부 part commit은 공개/ACK가 아니다. Publish는 모든 7개 part가 성공한 뒤 입력 순서로 호출한다. 실제 공개 transaction·fresh는 U06이다.

검사 근거: Prepare stub의 소유권/creation RED2, scheduler stub RED4, bound-store backend RED1 이후 같은 검사 GREEN. scheduler의 초기 GREEN 실행은 테스트 알림 채널 용량이 21개 작업보다 작아 timeout했고 helper를 충분한 bounded 용량으로 수정했다. 제품 오류로 분류하지 않는다. GOMAXPROCS=1, race, 단일 count=200은 전체 pkg/execution에서 통과했다. decoder custom budget RED1과 wire encoder/decoder budget 불일치 RED1도 수정 후 해당 모듈 race GREEN. 실제 노드/성능 측정 없음.

재현: `GOMAXPROCS=1 go test -race ./pkg/execution -count=200 -timeout=10m`; `go test -race ./pkg/chainfeed/evmdata`; execwire 모듈에서 `go test -race ./...`. 최종 통합 변경 후 필요한 전체/동시성 검사를 다시 완료하고 PR한다.

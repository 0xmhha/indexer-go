# Geth callTracer 형식 표본

`geth-v1.16.5-calltrace.json`은 go-ethereum v1.16.5의 [native callTracer](https://github.com/ethereum/go-ethereum/blob/v1.16.5/eth/tracers/native/call.go) hooks로 생성한 독립 결과다. decoder가 자기 출력으로 만든 golden이 아니다. full node·체인 EVM 실행·실제 Arc에서 수집한 실행 증거는 아니다.

mainnet chain config의 callTracer를 `onlyTopCall:false, withLog:false`로 만들고, 테스트 계정1·chain ID1의 서명된 fee-market 거래4개에 OnTxStart/OnEnter/OnExit/OnTxEnd를 제공했다. nonce0..3, value7..10, gas100000, gasUsed50000/60000/70000/80000, input01/02/03/04다. 목적지는 CALL/CREATE/CALL/CREATE 순서이며 뒤2개는 revert/out-of-gas다. 첫 거래는 revert하는 CALL·그 자식의 성공, DELEGATECALL, STATICCALL, SELFDESTRUCT를, 세 번째 거래는 root revert 아래 성공한 child를 포함한다. CREATE 실패의 to는 tracer가 생략한다.

테스트는 별도로 만든 signed transaction/body/receipt/header commitment와 최상위 결과를 대조한다. 하위 hooks는 의미/형식 시험을 위한 합성 입력이며 실제 EVM 실행을 증명하지 않는다. generator는 별도 임시 modfile에서 실행해 운영 go.mod/go.sum을 유지했고, 일반 검증에는 추가 tracer 의존성이 필요하지 않다. 생성 소스와 로그는 `/private/tmp/indexer-call-traces-20261016/native-fixture-generator_test.go` 및 `native-fixture-generation-final.log`에 보존한다.

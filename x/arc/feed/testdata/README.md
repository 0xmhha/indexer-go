# 기록된 Arc 실행 입력

`arc-67019625.json`은 Arc testnet5042002의 공개 RPC 기록에서 생성한 header/body/ETH68 receipt consensus RLP다. 블록 높이67,019,625, hash `0x8ab7d7c9d6c9822f9e271d025c54722bdf32632c618e7f2545ca720d26d14dbf`, 거래5개다. 각 RLP 바이트는 JSON base64로 저장한다. genesis/checkpoint는 식별용이며 이 fixture만으로 합의 finality를 증명하지 않는다.

원본 공개 RPC 기록은10/14의 `/private/tmp/indexer-arc-parity-recording-20261014`, RLP 변환은10/15의 `/private/tmp/indexer-node-feed-measure-20261015/fixture.json`이다. 변환 전체 입력의 SHA-256은 `b11f6c37a2be0831e3b3fad84c7e3ea577e1aa0808c9615785ad661b72b7ba70`이다. 기존 [기록 비교](../../../../docs/NODE_FEED.md#동일-arc-기록의-전송-경로-비교-1015)에서 사용한 입력 중 한 블록만 추출했다. 계정 잔액 map과 비밀 데이터는 포함하지 않는다.

회귀 시험은 블록 hash/optional header fields, 거래·receipt·withdrawal commitment, gas/bloom과 exact payload 보존을 확인한다. 실제 peer 패킷을 녹화한 자료가 아니며 P2P/공급 지연의 증거로 사용하지 않는다.

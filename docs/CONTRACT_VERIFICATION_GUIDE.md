# Contract Deployment & Verification Guide

이 가이드는 Forge를 사용하여 StableNet에 컨트랙트를 배포하고, indexer-go의 Etherscan-compatible API를 통해 소스코드를 검증하는 방법을 설명합니다.

## 목차

1. [사전 요구사항](#사전-요구사항)
2. [Foundry 설정](#foundry-설정)
3. [배포 스크립트 작성](#배포-스크립트-작성)
4. [배포 + 검증 실행](#배포--검증-실행)
5. [검증 상태 확인](#검증-상태-확인)
6. [트러블슈팅](#트러블슈팅)

---

## 사전 요구사항

- Foundry 설치 (`forge`, `cast`)
- 배포할 컨트랙트 소스코드
- StableNet RPC 엔드포인트
- Indexer API 엔드포인트

## Foundry 설정

### 1. `foundry.toml` 설정

```toml
[profile.default]
src = "src"
out = "out"
libs = ["lib"]
optimizer = true
optimizer_runs = 200
evm_version = "paris"

# StableNet 체인 설정
[rpc_endpoints]
stablenet = "${STABLENET_RPC_URL}"
stablenet_testnet = "${STABLENET_TESTNET_RPC_URL}"

# Etherscan-compatible 검증 설정
[etherscan]
stablenet = { key = "${ETHERSCAN_API_KEY}", url = "${INDEXER_API_URL}/api" }
stablenet_testnet = { key = "${ETHERSCAN_API_KEY}", url = "${INDEXER_TESTNET_API_URL}/api" }
```

### 2. `.env` 파일 설정

```bash
# RPC Endpoints
STABLENET_RPC_URL=http://localhost:8545
STABLENET_TESTNET_RPC_URL=https://testnet-rpc.stablenet.io

# Indexer API Endpoints
INDEXER_API_URL=http://localhost:8080
INDEXER_TESTNET_API_URL=https://testnet-indexer.stablenet.io

# API Key (아무 값이나 사용 가능)
ETHERSCAN_API_KEY=any

# 배포 계정
PRIVATE_KEY=0x...
```

---

## 배포 스크립트 작성

### 기본 배포 스크립트 (`script/Deploy.s.sol`)

```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import {Script, console} from "forge-std/Script.sol";
import {MyContract} from "../src/MyContract.sol";

contract DeployScript is Script {
    function setUp() public {}

    function run() public returns (MyContract) {
        uint256 deployerPrivateKey = vm.envUint("PRIVATE_KEY");

        vm.startBroadcast(deployerPrivateKey);

        MyContract myContract = new MyContract(
            // constructor arguments
        );

        console.log("MyContract deployed at:", address(myContract));

        vm.stopBroadcast();

        return myContract;
    }
}
```

### 여러 컨트랙트 배포 스크립트

```solidity
// SPDX-License-Identifier: MIT
pragma solidity ^0.8.19;

import {Script, console} from "forge-std/Script.sol";
import {TokenA} from "../src/TokenA.sol";
import {TokenB} from "../src/TokenB.sol";
import {Router} from "../src/Router.sol";

contract DeployAllScript is Script {
    function run() public {
        uint256 deployerPrivateKey = vm.envUint("PRIVATE_KEY");

        vm.startBroadcast(deployerPrivateKey);

        // 1. TokenA 배포
        TokenA tokenA = new TokenA("Token A", "TKA");
        console.log("TokenA:", address(tokenA));

        // 2. TokenB 배포
        TokenB tokenB = new TokenB("Token B", "TKB");
        console.log("TokenB:", address(tokenB));

        // 3. Router 배포 (TokenA, TokenB 주소 필요)
        Router router = new Router(address(tokenA), address(tokenB));
        console.log("Router:", address(router));

        vm.stopBroadcast();
    }
}
```

---

## 배포 + 검증 실행

### 방법 1: 배포와 검증 동시 실행 (권장)

```bash
# 환경 변수 로드
source .env

# 배포 + 검증 한 번에 실행
forge script script/Deploy.s.sol:DeployScript \
  --rpc-url $STABLENET_RPC_URL \
  --broadcast \
  --verify \
  --verifier-url $INDEXER_API_URL/api \
  --etherscan-api-key $ETHERSCAN_API_KEY \
  -vvvv
```

### 방법 2: 배포 후 별도 검증

```bash
# 1. 먼저 배포
forge script script/Deploy.s.sol:DeployScript \
  --rpc-url $STABLENET_RPC_URL \
  --broadcast \
  -vvvv

# 2. 배포된 주소로 검증
forge verify-contract \
  --verifier-url $INDEXER_API_URL/api \
  --etherscan-api-key $ETHERSCAN_API_KEY \
  --compiler-version v0.8.19 \
  --optimizer-runs 200 \
  <DEPLOYED_ADDRESS> \
  src/MyContract.sol:MyContract
```

### 방법 3: Constructor Arguments가 있는 경우

```bash
# Constructor arguments를 ABI 인코딩
CONSTRUCTOR_ARGS=$(cast abi-encode "constructor(string,string)" "Token Name" "TKN")

forge verify-contract \
  --verifier-url $INDEXER_API_URL/api \
  --etherscan-api-key $ETHERSCAN_API_KEY \
  --constructor-args $CONSTRUCTOR_ARGS \
  <DEPLOYED_ADDRESS> \
  src/MyToken.sol:MyToken
```

---

## Bash 스크립트 예제

### `scripts/deploy-and-verify.sh`

```bash
#!/bin/bash
set -e

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# 환경 변수 로드
if [ -f .env ]; then
    source .env
else
    echo -e "${RED}Error: .env file not found${NC}"
    exit 1
fi

# 인자 파싱
SCRIPT_NAME=${1:-"Deploy.s.sol:DeployScript"}
NETWORK=${2:-"stablenet"}

# 네트워크별 설정
case $NETWORK in
    "stablenet")
        RPC_URL=$STABLENET_RPC_URL
        VERIFIER_URL=$INDEXER_API_URL/api
        ;;
    "testnet")
        RPC_URL=$STABLENET_TESTNET_RPC_URL
        VERIFIER_URL=$INDEXER_TESTNET_API_URL/api
        ;;
    *)
        echo -e "${RED}Unknown network: $NETWORK${NC}"
        exit 1
        ;;
esac

echo -e "${YELLOW}========================================${NC}"
echo -e "${YELLOW}Deploying to: $NETWORK${NC}"
echo -e "${YELLOW}RPC URL: $RPC_URL${NC}"
echo -e "${YELLOW}Verifier URL: $VERIFIER_URL${NC}"
echo -e "${YELLOW}Script: $SCRIPT_NAME${NC}"
echo -e "${YELLOW}========================================${NC}"

# 배포 + 검증 실행
forge script script/$SCRIPT_NAME \
    --rpc-url $RPC_URL \
    --broadcast \
    --verify \
    --verifier-url $VERIFIER_URL \
    --etherscan-api-key $ETHERSCAN_API_KEY \
    -vvvv

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Deployment and verification complete!${NC}"
echo -e "${GREEN}========================================${NC}"
```

### `scripts/verify-existing.sh`

```bash
#!/bin/bash
set -e

# 사용법 확인
if [ "$#" -lt 2 ]; then
    echo "Usage: $0 <CONTRACT_ADDRESS> <CONTRACT_PATH:NAME> [CONSTRUCTOR_ARGS]"
    echo "Example: $0 0x1234... src/MyToken.sol:MyToken"
    echo "Example with args: $0 0x1234... src/MyToken.sol:MyToken \$(cast abi-encode 'constructor(string)' 'MyToken')"
    exit 1
fi

source .env

CONTRACT_ADDRESS=$1
CONTRACT_NAME=$2
CONSTRUCTOR_ARGS=${3:-""}

echo "Verifying contract at $CONTRACT_ADDRESS..."

VERIFY_CMD="forge verify-contract \
    --verifier-url $INDEXER_API_URL/api \
    --etherscan-api-key $ETHERSCAN_API_KEY \
    --watch"

if [ -n "$CONSTRUCTOR_ARGS" ]; then
    VERIFY_CMD="$VERIFY_CMD --constructor-args $CONSTRUCTOR_ARGS"
fi

VERIFY_CMD="$VERIFY_CMD $CONTRACT_ADDRESS $CONTRACT_NAME"

eval $VERIFY_CMD

echo "Verification submitted!"
```

---

## Makefile 예제

```makefile
-include .env

.PHONY: deploy verify deploy-verify

# 기본 설정
NETWORK ?= stablenet
SCRIPT ?= Deploy.s.sol:DeployScript

# 네트워크별 URL 설정
ifeq ($(NETWORK),stablenet)
    RPC_URL := $(STABLENET_RPC_URL)
    VERIFIER_URL := $(INDEXER_API_URL)/api
else ifeq ($(NETWORK),testnet)
    RPC_URL := $(STABLENET_TESTNET_RPC_URL)
    VERIFIER_URL := $(INDEXER_TESTNET_API_URL)/api
endif

# 배포만
deploy:
	forge script script/$(SCRIPT) \
		--rpc-url $(RPC_URL) \
		--broadcast \
		-vvvv

# 배포 + 검증
deploy-verify:
	forge script script/$(SCRIPT) \
		--rpc-url $(RPC_URL) \
		--broadcast \
		--verify \
		--verifier-url $(VERIFIER_URL) \
		--etherscan-api-key $(ETHERSCAN_API_KEY) \
		-vvvv

# 기존 컨트랙트 검증
verify:
	@echo "Usage: make verify ADDRESS=0x... CONTRACT=src/MyContract.sol:MyContract"
	forge verify-contract \
		--verifier-url $(VERIFIER_URL) \
		--etherscan-api-key $(ETHERSCAN_API_KEY) \
		--watch \
		$(ADDRESS) \
		$(CONTRACT)

# 검증 상태 확인
check-verify:
	@curl -s "$(VERIFIER_URL)?module=contract&action=getsourcecode&address=$(ADDRESS)" | jq
```

**사용 예:**
```bash
# testnet에 배포 + 검증
make deploy-verify NETWORK=testnet

# 기존 컨트랙트 검증
make verify ADDRESS=0x1234... CONTRACT=src/MyToken.sol:MyToken

# 검증 상태 확인
make check-verify ADDRESS=0x1234...
```

---

## 검증 상태 확인

### API로 직접 확인

```bash
# 소스코드 조회
curl "$INDEXER_API_URL/api?module=contract&action=getsourcecode&address=<ADDRESS>"

# ABI 조회
curl "$INDEXER_API_URL/api?module=contract&action=getabi&address=<ADDRESS>"

# 검증 상태 확인 (GUID 필요)
curl "$INDEXER_API_URL/api?module=contract&action=checkverifystatus&guid=<GUID>"
```

### GraphQL로 확인

```graphql
query {
  contractVerification(address: "0x...") {
    address
    isVerified
    name
    compilerVersion
    optimizationEnabled
    optimizationRuns
    sourceCode
    abi
    verifiedAt
    licenseType
  }
}
```

### Frontend에서 확인

브라우저에서 `http://localhost:3000/address/<CONTRACT_ADDRESS>` 접속

---

## 트러블슈팅

### 1. "Bytecode mismatch" 에러

**원인:** 컴파일 설정이 배포 시와 다름

**해결:**
```bash
# foundry.toml 설정 확인
optimizer = true
optimizer_runs = 200
evm_version = "paris"

# 동일한 컴파일러 버전 사용
solc = "0.8.19"
```

### 2. "Contract source code not verified" 에러

**원인:** 검증이 아직 진행 중이거나 실패

**해결:**
```bash
# 검증 상태 확인
forge verify-contract --watch ...

# 또는 API로 확인
curl "$INDEXER_API_URL/api?module=contract&action=checkverifystatus&guid=<GUID>"
```

### 3. Constructor Arguments 인코딩 문제

**해결:**
```bash
# cast를 사용하여 올바르게 인코딩
cast abi-encode "constructor(address,uint256)" 0x1234... 1000

# 복잡한 타입의 경우
cast abi-encode "constructor(address[])" "[0x1234...,0x5678...]"
```

### 4. "Verifier not configured" 에러

**원인:** indexer-go에 Verifier가 설정되지 않음

**해결:** indexer-go 시작 시 Verifier 옵션 확인

### 5. Flattened 소스코드가 필요한 경우

```bash
# 소스코드 flatten
forge flatten src/MyContract.sol > flattened/MyContract.sol

# flatten된 파일로 검증
forge verify-contract \
  --verifier-url $INDEXER_API_URL/api \
  --etherscan-api-key any \
  <ADDRESS> \
  flattened/MyContract.sol:MyContract
```

---

## API Reference

### Etherscan-compatible Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api?module=contract&action=verifysourcecode` | 소스코드 검증 제출 |
| GET | `/api?module=contract&action=checkverifystatus&guid=xxx` | 검증 상태 확인 |
| GET | `/api?module=contract&action=getabi&address=xxx` | ABI 조회 |
| GET | `/api?module=contract&action=getsourcecode&address=xxx` | 소스코드 조회 |

### Request Parameters (verifysourcecode)

| Parameter | Required | Description |
|-----------|----------|-------------|
| contractaddress | Yes | 컨트랙트 주소 |
| sourceCode | Yes | Solidity 소스코드 |
| contractname | No | 컨트랙트 이름 |
| compilerversion | Yes | 컴파일러 버전 (예: v0.8.19) |
| optimizationUsed | No | 최적화 사용 여부 (1 또는 0) |
| runs | No | 최적화 runs (기본값: 200) |
| constructorArguments | No | Constructor arguments (hex) |
| licenseType | No | 라이센스 타입 |

### Response Format

```json
{
  "status": "1",
  "message": "OK",
  "result": "guid-string-here"
}
```

---

## 참고 자료

- [Foundry Book - Deploying](https://book.getfoundry.sh/forge/deploying)
- [Foundry Book - Verifying](https://book.getfoundry.sh/forge/verification)
- [Etherscan API Documentation](https://docs.etherscan.io/api-endpoints/contracts)

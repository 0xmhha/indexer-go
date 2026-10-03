# indexer-go 코드 그래프 (10/3)

커밋 `5baff64` 기준으로 모듈 전체를 타입 정보까지 읽어 만든 코드 그래프다. 리팩토링 계획([refactoring-plan.md](../refactoring-plan.md))의 수치는 모두 이 그래프에서 나왔다.

## 파일

| 파일 | 내용 |
|---|---|
| `graph.json.gz` | 그래프 원본(gzip, 풀면 약 4MB). `nodes`, `edges`, `concrete_assertions` |
| `analysis.txt` | 패키지별 크기·import 폐포·fan-in/out, 동시성 신호, 큰 함수, 큰 타입, 인터페이스, 구체 타입 단언, KVStore 직접 호출 |
| `coupling.txt` | 패키지 밖에서 쓰는 저장소 인터페이스, 패키지 import mermaid, 선언 폐포 |

## 만드는 방법

도구는 `tools/astgraph`에 있다. 메인 모듈과 분리된 별도 Go 모듈이라 `go build ./...`나 lint에 들어가지 않는다.

```bash
cd tools/astgraph
go build -o /tmp/astgraph .
/tmp/astgraph -dir ../.. -out /tmp/astgraph-out          # graph.json 생성 (테스트 파일 제외)
python3 metrics.py  /tmp/astgraph-out/graph.json  > analysis.txt
python3 coupling.py /tmp/astgraph-out/graph.json  > coupling.txt
```

대상 모듈의 `go.mod`를 바꾸지 않도록 `GOWORK=off GOFLAGS=-mod=readonly`로 읽는다.

## 그래프 모델

| 노드 종류 | 수 | 뜻 |
|---|---|---|
| package | 42 | Go 패키지 |
| func | 763 | 패키지 함수 |
| method | 1,737 | 구체 타입의 메서드 |
| type | 520 | 인터페이스가 아닌 이름 있는 타입 |
| interface | 94 | 메서드가 있는 인터페이스 |
| iface_method | 815 | 인터페이스 메서드 |

| 간선 종류 | 수 | 뜻 |
|---|---|---|
| import | 103 | 모듈 안 패키지 import |
| ext_import | 79 | 외부 모듈 import |
| call | 2,583 | 정적으로 대상이 정해지는 호출 |
| iface_call | 635 | 인터페이스 메서드 호출(대상 구현은 정해지지 않음) |
| ref | 7,896 | 타입·함수 값 참조 |
| implements | 156 | 구체 타입(또는 그 포인터)이 인터페이스를 구현 |
| contains | 3,929 | 패키지·인터페이스가 선언을 포함 |

노드에는 동시성·비용 신호도 붙어 있다. `go_stmts`, `selects`, `chan_ops`, `make_chan`, `locks`, `sleeps`, `atomics`, `sprintf`, `json`이다. `concrete_assertions`는 모듈 안의 구체 타입으로 타입 단언하는 위치다.

## 한계

- 인터페이스를 거친 호출은 `iface_call`로 인터페이스 메서드까지만 잇는다. 실제 구현으로 가는 간선은 없으므로 호출 폐포는 실제보다 작게 나올 수 있다.
- 선언 폐포는 "타입을 참조하면 그 타입의 메서드 전부가 따라온다"고 계산한다. 그래서 `Fetcher`의 메서드 하나에서 출발해도 `Fetcher` 전체가 딸려 온다. 기존 문서의 gap 복구 폐포(5,997줄)와 이 그래프의 값(7,479줄)이 다른 것은 이 계산 규칙의 차이다.
- 리플렉션, `init()` 등록, 빌드 태그로 갈리는 코드는 정적으로 보이는 범위까지만 잡힌다.

# Native Plugin 실행 갭 수정 — 진행 상태 (WIP)

> 작성 2026-07-08. 목표: "A(빌드 캐시)부터 고쳐 native 커스텀 stage 샘플이 e2e 로 통과하게 하고, 드러나는 수정사항 모두 수정."
> 세션이 끊겨도 이어서 검증할 수 있도록 상태를 남긴다.

## 배경 (왜 이 작업을 하나)

native(compile-in) 커스텀 stage 는 seed·e2e 어디서도 실행된 적 없는 사각지대였고, 재현 결과 3중 갭이 확정됐다([[native-plugin-e2e-gaps]] memory 참조):
- **A. 빌드 실패**: control-plane pod(uid=1000, HOME=/)에서 `go mod tidy` 가 `mkdir /.cache: permission denied`.
- **A-2. 소스 부재**: runtime 이미지에 pipeline-core/shared 소스가 없어 replace(`../pipeline-core`)가 깨짐.
- **B. 실행 이미지 부재**: DockerPush=false → 빌드 바이너리를 담은 이미지가 레지스트리에 없음 → Job ImagePullBackOff.
- **C. realtime 미지원**: agent 바이너리에 registry_custom.go 없음 → native stage 를 조용히 passthrough(무시).

## 확실하게 된 것 (코드 완료 + 단위테스트 통과, 커밋 前)

모두 **컴파일 + 관련 단위테스트 통과** 확인함. 아직 **커밋 안 됨**, **e2e 실동작 미완**.

1. **A. 빌드 캐시** — `runner_builder.go runCommand`: GOCACHE/GOPATH/HOME 을 tmpDir 하위로 주입. pod 안에서 `go mod tidy` 성공 재현 확인.
2. **A-2. 소스 포함** — `Dockerfile.control-plane`: runtime 에 `COPY pipeline-core /app/pipeline-core`, `COPY shared /app/shared` 추가 + `ENV CONDUIX_SOURCE_ROOT=/app`. `GenerateRunnerGoMod(plugins, sourceRoot)` 가 replace 를 절대경로(`/app/pipeline-core`)로 생성.
3. **B. 바이너리 BLOB 주입** (선택지 2 — 레지스트리 push 없이):
   - `models.go RunnerVersion`: `Binary []byte(longblob, json:"-")`, `BinarySize int` 추가.
   - `runner_builder.go`: go build 후 바이너리를 gzip 압축해 `version.Binary` 저장.
   - `runner_handler.go DownloadBinary` + `routes.go` internal 그룹에 `GET /api/v1/internal/runner/versions/:id/binary`(인증면제, gzip 스트리밍).
   - `runner_resolver.go ResolveRunnerVersion`: native 면 최신 ready RunnerVersion ID+image 반환. 변경분/미빌드/**바이너리없음**이면 BuildRequiredError.
   - `shared/types/agent.go WorkflowExecutionCommand.RunnerVersionID` 필드 추가.
   - `workflow.go StartWorkflow`: 트랜잭션 안에서 ResolveRunnerVersion 호출 → BuildRequired 면 롤백+409, realtime+native 면 400 차단. versionID 를 cmd 에 전달.
   - `workflow_partition.go publishSubExecutions`: runnerVersionID 파라미터 추가 → 각 sub cmd 에 전달.
   - `agent.go delegateBatchJob`: JobSpec 에 RunnerVersionID 전달.
   - `job_manager.go CreateBatchJob`: RunnerVersionID 있으면 initContainer(wget→gunzip→/runner/pipeline-batch-job, chmod+x) + emptyDir + main container command override. base/init 이미지는 batch-job(alpine, wget/gunzip 내장) 재사용 → e2e pull 회피.
4. **C. realtime 차단** — 위 workflow.go 의 REALTIME_NATIVE_UNSUPPORTED(400). realtime 은 in-process 라 바이너리 주입 불가 → native 쓰는 realtime 은 실행 차단(명확한 에러). realtime 실행 지원은 비목표.

## 미확실 / 남은 것 (e2e 실동작 아직 미검증)

**핵심: native stage 가 실제로 Job 에서 실행되어 데이터에 반영되는지 아직 한 번도 성공 못 함.**

### 발견·수정한 결함 (2026-07-08 세션)
5. **좀비 building 락** — ✅ 수정 완료. `runner_builder.go Build()` 진입 시 `2*BuildTimeout` 넘긴 building 을 failed 로 self-heal 회수(stale grace) 후 락 판정. 부팅 정리 불필요.
6. **runner 가 스텁 main 이라 파이프라인 미실행** — ✅ 수정 완료(핵심). 빌더가 만들던 바이너리는 `stage 목록 출력 후 select{}` 로 멈추는 스텁이었다(`generateRunnerMain`, 옛 `conduix-runner` 모듈). native stage 를 compile-in 해도 **실제 파이프라인을 실행하지 않아** orders_out 이 안 채워짐. → 빌더가 `pipeline-batch-job` 모듈을 tmpDir 로 복사하고 `cmd/runner/registry_custom.go` 주입 + go.mod 플러그인 require/replace 추가 후 `./cmd/runner`(실제 배치 실행 로직)를 빌드하도록 재설계. 로컬 검증: 121MB arm64 바이너리에 `pricedouble.Stage.(Init/Process/Close)` 심볼 링크 확인(`go tool nm`). Dockerfile.control-plane 에 `COPY pipeline-batch-job` 추가.
7. **빌드가 3시간+ 걸린 진짜 원인 = dockerd 무한 스핀(containerd image store dangling 충돌)** — ✅ 수정 완료. CPU/emulation/리소스 문제가 아니었다.
   - **증상**: buildx `unpacking` 단계가 1650s 멈춤. tiny/golang-base 이미지는 unpacking 0.0~0.1s(정상) → 크기·buildkit 문제 아님.
   - **결정적 관측**: 빌드를 전부 죽여도 dockerd 가 **상시 136% CPU**(누적 52h). buildkitd 는 CPU 0.0%. 즉 unpacking 이 dockerd 스핀에 CPU 를 뺏겨 굶었다.
   - **근본 원인**: `journalctl -u docker` 에 **10초마다** `multiple images have the same target, but one of them is still dangling`(rancher metrics-server/coredns/local-path-provisioner). containerd image store(`overlayfs` + `io.containerd.snapshotter.v1`)에 **정상 태그와 `moby-dangling@sha256:` 그림자가 같은 digest 를 가리키는 충돌**이 있어 dockerd 가 정리 재시도를 무한 반복 → CPU 스핀. `docker image prune` 은 이 dangling 을 못 지운다(k3s 사용중 digest 와 충돌해 "사용중"으로 보임).
   - **수정**: `colima ssh -- sudo systemctl restart docker` 로 dockerd 재시작 → store dangling 참조 재계산. **CPU 136%→18%, dangling 경고 0건, unpacking 즉시 정상화.** (주의: docker 재시작 시 conduix pod 가 잠깐 재생성됨 — mysql 보다 control-plane 이 먼저 떠 DB 연결 실패로 CrashLoopBackOff 가능하나 재시도로 복구.)
   - 부수 최적화: gopls `@latest`→`@v0.22.0` 고정(캐시 안정), 빌더 GOCACHE/GOPATH 를 tmpDir 밖 영속 CacheDir 로(재빌드 캐시 재사용), `Makefile e2e-images` 빌드 전 `docker image prune -f`(dangling 누적 완화 — 단 위 store 충돌은 이걸로 안 풀리고 dockerd 재시작 필요).
8. **소스 파일 0600 권한 → RunnerBuilder 복사 실패** — ✅ 수정 완료. 리포에 0600 소스(.go 35개 포함)가 `COPY` 로 그대로 들어와 uid=1000 프로세스가 못 읽음(`permission denied: /app/pipeline-core/Makefile`). Dockerfile.control-plane 에 `RUN chmod -R a+rX /app/{plugin-sdk,pipeline-core,shared,pipeline-batch-job}` 추가.
9. **control-plane CPU limit 500m → native 빌드가 cgroup throttle 로 10분+** — ✅ 수정 완료. control-plane 이 컨테이너 안에서 go build(pipeline-batch-job 전체 컴파일)를 하는데 기본 limit 500m(0.5코어)이라 컴파일이 CPU throttle 로 굶음(compile 프로세스 etime 3:25 인데 CPU time 0:05). values-e2e.yaml controlPlane.resources 를 cpu 2 / mem 2Gi 로 상향. (2코어 빌드 실측 216s.)
10. **binary Save 에러 무시 → status building 갇힘** — ✅ 수정 완료. 빌드는 `ready` 로그가 나왔는데 DB 는 building + binary_size=0 + build_log 빈값. 원인: `version.Binary`(gzip 수십MB)를 status/메타데이터와 한 `db.Save` 로 쓰다 큰 write 가 드라이버 기본 maxAllowedPacket(64MB) 한도에 걸려 통째로 실패, Save 반환에러를 안 잡아 building 에 갇힘. 수정: (a) `persistBinary` 로 binary 만 별도 UPDATE, status Save 는 `Omit("binary")` 로 큰 데이터 재전송 방지, (b) 모든 Save 에러 체크·실패 시 failed 확정(좀비 방지), (c) DSN 에 `maxAllowedPacket=0`(서버 설정 따름). → 이후 `binary_size=121766050 ready` 저장 성공 확인.
11. **binary 라우트 경로 불일치 → initContainer 404** — ✅ 수정 완료. initContainer wget URL 은 `/api/v1/internal/runner/versions/:id/binary` 인데 routes.go 는 `/api/v1/runner/...`(internal 없음)로 등록 → 404 → gunzip invalid magic → Init:Error. routes.go 그룹을 `/internal/runner` 로 맞춤.
12. **동시 빌드 race → GOCACHE 교착(hang)** — ✅ 수정 완료. plugin update 의 auto-build(goroutine)와 수동 `POST /runner/build`(goroutine)가 거의 동시에 `Build()` 호출. DB building-count 락은 체크→레코드생성이 비원자적이라 둘 다 통과 → 두 go build 가 같은 영속 CacheDir(GOCACHE)을 동시에 써 compile 프로세스가 defunct 로 hang(CPU time 안 늘고 캐시 정지). 패키지 전역 `buildMu sync.Mutex` + `TryLock`(대기 없이 두 번째 즉시 거부)으로 직렬화. (이전 성공은 auto-build 가 우연히 먼저 실패한 타이밍이었음.)
13. **native stage 가 데이터 미변경(passthrough)** — ✅ 원인규명. 빌드·실행·initContainer 다 정상인데 orders_out.price 가 원본 그대로였다. 원인: SQL source(`sql.go:262`)가 driver `[]byte`(DECIMAL)를 **string 으로 변환**해 record["price"] 가 `"19.99"` string 인데, 검증 plugin 은 `case float64` 만 처리 → 타입 미매치로 passthrough. **시스템 버그 아님** — plugin 이 SQL 타입을 처리 안 한 것. 검증 plugin 에 `case string { ParseFloat }` 추가. (관측성 개선: executor `applyStreamStage` 미등록 타입에 warn-once 로그 추가 — 이 경우엔 등록은 됐고 타입만 안 맞아 경고가 안 났고, 그로써 "등록됐는데 데이터만 안 바뀜"을 좁힐 수 있었다.)

### 검증 남은 항목 (수정 배포 후 순서대로)
- [ ] control-plane + agent 이미지 재빌드·재배포 (B·좀비락 수정 반영).
- [ ] native plugin 빌드가 **ready + binary_size>0** 로 성공하는지 (A·A-2 실증). *현재 e2e 배포엔 A·A-2 반영된 이미지가 떠 있으나 좀비 락에 막혀 빌드 자체를 못 돌려봄.*
- [ ] native stage 를 쓰는 **bulk 워크플로우 실행 → orders_out.price 가 2배**(native stage 실동작 증명). 검증 스크립트 `/tmp/verify_native_e2e.sh` (pricedouble = price x2, id1 19.99→39.98 / id2 5.50→11.00 / id3 120→240).
- [ ] initContainer 가 CP 에서 바이너리를 실제로 받아 실행하는지 (Job pod describe/logs).
- [ ] realtime + native 트리거 시 400 차단 확인.
- [ ] 회귀: 기존 js_script 워크플로우·파티션 분산 여전히 정상.

### 알려진 위험 (Plan 단계에서 지적, 미해결)
- **initContainer 실패 시 execution 영구 running**: initContainer(wget/gunzip)가 실패하면 main container 가 안 떠 CALLBACK 이 없고, control-plane execution 이 running 에 갇힘. Job watch 또는 stuck-execution 정리 필요(별도).
- **바이너리 크기**: `DownloadBinary` 가 전체를 메모리 로드 후 응답 → 동시 sub N개면 메모리 스파이크. gzip 이라 전송량은 줄지만 스트리밍 최적화는 후속.
- **MySQL max_allowed_packet**: longblob 수십MB write/read 가 패킷 한도 초과 가능 — 빌드/다운로드 실패 시 확인.

## 커밋 안 된 변경 파일 (git status, 2026-07-08)
```
control-plane/internal/api/handlers/runner_handler.go   (DownloadBinary)
control-plane/internal/api/handlers/workflow.go         (resolve+차단)
control-plane/internal/api/handlers/workflow_partition.go (runnerVersionID 전달)
control-plane/internal/api/routes.go                    (internal binary 라우트)
control-plane/internal/builder/runner_builder.go        (GOCACHE, sourceRoot, gzip binary 저장)
control-plane/internal/builder/runner_builder_test.go   (시그니처)
control-plane/internal/services/runner_resolver.go      (ResolveRunnerVersion)
control-plane/pkg/models/models.go                      (Binary/BinarySize)
deploy/docker/Dockerfile.control-plane                  (소스 COPY + CONDUIX_SOURCE_ROOT)
pipeline-worker/internal/agent/agent.go                 (RunnerVersionID 전달)
pipeline-worker/internal/k8s/job_manager.go             (initContainer 주입)
shared/types/agent.go                                   (RunnerVersionID)
docs/BULK_VS_REALTIME_COMPARISON.md                     (세션 무관 — 커밋 제외)
```

## 다음 즉시 할 일
1. 좀비 building 락 코드 수정(부팅 시 정리 + stale grace).
2. control-plane + agent 재빌드·재배포.
3. `/tmp/verify_native_e2e.sh` 재실행 → 빌드 ready + price 2배 확인.
4. 통과하면: 커밋(버그별 분리) + e2e seed 에 native 샘플 영구 추가(#13) + memory 갱신.

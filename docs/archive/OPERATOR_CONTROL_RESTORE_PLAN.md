운영자 제어/모니터링 복원 작업 지시서

    배경: actor 실행 엔진 제거 시 agent의 개별 파이프라인 REST(GET/POST /pipelines/:id/{start,stop,pause,resume,status}, GET /pipelines)를 함께 삭제함. 이건 운영자가 외부 REST로 파이프라인을 모니터링/제어하던 표면이었음. 삭제 판단의 "레포 내 호출자 없음" 근거는 오류였음 — 운영자용 외부 API는 코드 내 호출자가 없는 게 정상.

    확정 결론: 개별 파이프라인 REST를 원형 복원하지 않음. 워크플로우(그룹) 단위 제어로 일원화(엔진 이중화 재발 방지) + 동적 유량제어만 신규 구현.

    Step 1 — 검증(코드로 먼저 확인, 구현 전):
    1. web-ui/src/pages, web-ui/src/services/api.ts에서 모니터링 화면·제어 UI 존재 및 연결 API grep.
    2. agent GET /monitoring, /monitoring/:executionId 응답에 실행상태 + 파이프라인별 처리량(StatsCollector 통계) 포함 여부.
    3. control-plane POST /workflows/:id/{stop,pause,resume}가 web-ui 버튼에 연결됐는지.

    Step 2 — 판정: 연결돼 있으면 UI 보강만, 비어있으면 기존 워크플로우 API에 UI 연결(신규 API 부활 아님).

    Step 3 — 신규 구현(진짜 공백): 동적 유량제어. Redis 명령 채널(cluster:<id>:commands)에 throttle 조정 액션 추가 → agent handleCommand → GroupExecutor의 throttle rate 런타임 갱신. control-plane POST /workflows/:id/throttle + web-ui 컨트롤. (throttle stage는 이미 존재)

    Step 4 — 완료 정의: web-ui에서 ①실행상태 ②처리량 ③유량제어 ④중단 모두 가능, 전 모듈 빌드·테스트 green, 브라우저 확인.

사용자 확인 필요: agent standalone(control-plane 없이 REST 직접 제어) 용례 유지 여부. 유지 시 개별 파이프라인 제어 표면을 GroupExecutor 기반으로 별도 노출 필요.         
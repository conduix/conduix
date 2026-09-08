package geocode_kakao

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// --- 주소 정규화 (문서 '주소 정규화 규칙'의 예시 그대로) ---

func TestNormalizeAddress(t *testing.T) {
	tests := []struct{ in, want string }{
		{"경기도 남양주시 도농1길 69, 화장실", "경기도 남양주시 도농1길 69"},
		{"서울 강북구 삼양로 78 (길음동)", "서울 강북구 삼양로 78"},
		{"대전 유성구 구즉로58번길15-12", "대전 유성구 구즉로58번길 15-12"},
		{"서울  강남구   테헤란로  1", "서울 강남구 테헤란로 1"},
		{"  ", ""},
		{"경기도 평택시 고덕로 283(좌교리", "경기도 평택시 고덕로 283(좌교리"}, // 안 닫힌 괄호 보존 → 파손 판정
		// 회귀: 괄호 안에 쉼표가 있으면 예전엔 쉼표를 먼저 잘라 '(소태동' 으로 깨졌다. 괄호쌍을 먼저 지운다.
		{"전남광주통합특별시 동구 학소로 109 (소태동, 무등산골드클래스)", "전남광주통합특별시 동구 학소로 109"},
		{"서울특별시 성북구 종암로 58(종암동)", "서울특별시 성북구 종암로 58"},
		// 서술 접미어는 통째로 버리지 않고 떼어 번지를 남긴다.
		{"강원특별자치도 고성군 거진읍 거진항1길 4 인근", "강원특별자치도 고성군 거진읍 거진항1길 4"},
	}
	for _, tt := range tests {
		if got := normalizeAddress(tt.in); got != tt.want {
			t.Errorf("normalizeAddress(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// isUnfixableAddress 는 normalize 를 거친 norm 을 받는 전제이므로, 케이스도 normalize 결과로 준다.
// 판정 기준: 구체적 위치(번지/도로+번호)가 남아있으면 시도, 건물명·서술만 남으면 불가.
func TestIsUnfixableAddress(t *testing.T) {
	unfixable := []string{
		"경기도 성남시 수정구 태평인라인장",       // 서술어(앞) 제거 후 — 건물명만, 번지 없음
		"경상북도 포항시 북구 송라면 조사리",       // 서술어(해안가) 제거 후 — 리 이름만, 번지 없음
		"강원특별자치도 횡성군 우천면 우항리 583-2외 8필지", // 외 N필지
		"경기도 평택시 고덕면 고덕로 283(좌교리",   // 안 닫힌 괄호(파손)
		"강원특별자치도 임계면 송계리",            // 리 이름만
		"경기도 고양시 일산동구",                // 행정구역만
		// 번지 없는 도로명은 의도적으로 불가 — 카카오가 도로 전체 대표좌표(address_type=ROAD)를
		// 주고 후보가 10건씩 나와, 화장실이 아닌 위치가 박힌다.
		"강원특별자치도 강릉시 남부로",
		"강원특별자치도 강릉시 강동면 율곡로",
	}
	fixable := []string{
		"강원특별자치도 양양군 현남면 인구길 33", // 도로명+번지
		"인천광역시 서구 가좌동 399",          // 지번(동+번지)
		"가좌4동 399",                     // sido 접두 없는 지번 — 형태만 맞으면 시도
		"남문로 352",                      // sido 접두 없는 도로명 — 시도
		"전남광주통합특별시 곡성군 삼기면 곡순로 1436",
		"강원특별자치도 고성군 거진읍 거진항1길 4", // '인근' 제거 후 번지 남음
		// 아래는 2026-09-08 실측으로 드러난 누락 패턴 — 전부 카카오 API 가 찾는 주소인데
		// reAddressShape 가 걸러내 API 호출조차 하지 않았다(실패 3,157건의 대부분).
		"강원특별자치도 동해시 공단 7로 31",        // 로 앞이 숫자(원본에 공백 섞임, 실제 '공단7로')
		"강원특별자치도 정선군 정선읍 녹송 8길 55",    // 길 앞이 숫자
		"강원특별자치도 정선군 여량면 여량 3리 649-1", // 리 앞이 숫자
		"강원특별자치도 동해시 구호동239",           // 동+번지 공백 없음
		"강원특별자치도 동해시 달방동 산 3-6",        // 산번지(최대 누락 덩어리)
		"경기도 광명시 가학동 산14-1",             // 산번지 공백 없음
		"강원특별자치도 고성군 현내면 757-5",        // 동/리 없이 번지만
	}
	for _, a := range unfixable {
		if !isUnfixableAddress(a) {
			t.Errorf("expected unfixable: %q", a)
		}
	}
	for _, a := range fixable {
		if isUnfixableAddress(a) {
			t.Errorf("expected fixable: %q", a)
		}
	}
}

func TestVariants(t *testing.T) {
	if got := spacingVariants("서울 강서구 수리골길17"); len(got) == 0 || got[0] != "서울 강서구 수리골길 17" {
		t.Errorf("spacing detach: %v", got)
	}
	if got := lotNotationVariant("전남 곡성군 죽곡면 원달리 산 52번지 18호"); got != "전남 곡성군 죽곡면 원달리 산52-18" {
		t.Errorf("lot notation: %q", got)
	}
	if got := sublotVariants("인천 서해구 가좌동 399"); len(got) != 5 || got[0] != "인천 서해구 가좌동 399-1" {
		t.Errorf("sublot: %v", got)
	}
	if got := sublotVariants("인천 서해구 가좌동 399-1"); got != nil {
		t.Errorf("sublot on existing sublot: %v", got)
	}
}

// --- 플러그인 인터페이스 흐름 (httptest 모의 카카오) ---

func mockKakao(t *testing.T, handler func(q string) (lat, lon string, road, found bool)) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	calls := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "KakaoAK test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		lat, lon, road, found := handler(r.URL.Query().Get("query"))
		docs := []any{}
		if found {
			doc := map[string]any{"x": lon, "y": lat,
				"address": map[string]any{"region_1depth_name": "인천", "region_2depth_name": "서구"}}
			if road {
				doc["road_address"] = map[string]any{}
			}
			docs = append(docs, doc)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"documents": docs})
	}))
	return srv, calls
}

func newTestStage(t *testing.T, baseURL, apiKey string) *Stage {
	t.Helper()
	s := &Stage{}
	err := s.Init(map[string]any{
		"api_key": apiKey, "api_base_url": baseURL,
		"address_field": "road_addr", "lotno_field": "lotno_addr",
		"rps": 1000.0,
	})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	return s
}

// mockNaver NCP Maps Geocoding 모의 서버. found=true 면 addresses[] 에 좌표 1건 반환.
func mockNaver(t *testing.T, handler func(q string) (lat, lon string, road, found bool)) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	calls := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("x-ncp-apigw-api-key-id") != "nid" || r.Header.Get("x-ncp-apigw-api-key") != "nsecret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		lat, lon, road, found := handler(r.URL.Query().Get("query"))
		addrs := []any{}
		if found {
			// addressElements 에 LAND_NUMBER 를 넣어야 "번지까지 특정" 으로 인정된다
			// (동 중심점 거부 로직). 실제 응답과 같은 형태를 유지한다.
			a := map[string]any{"x": lon, "y": lat, "jibunAddress": "지번",
				"addressElements": []any{
					map[string]any{"types": []string{"LAND_NUMBER"}, "longName": "123-4"},
				}}
			if road {
				a["roadAddress"] = "도로명"
			}
			addrs = append(addrs, a)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "OK", "addresses": addrs})
	}))
	return srv, calls
}

// 카카오가 못 찾은 주소를 네이버 폴백이 찾는다.
func TestProcess_NaverFallbackFindsWhatKakaoMissed(t *testing.T) {
	ksrv, kcalls := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "", "", false, false // 카카오는 전부 not_found
	})
	defer ksrv.Close()
	nsrv, ncalls := mockNaver(t, func(q string) (string, string, bool, bool) {
		return "37.8668522", "127.7211905", true, true // 네이버는 찾음(한반도 범위 내)
	})
	defer nsrv.Close()

	s := &Stage{}
	if err := s.Init(map[string]any{
		"api_key": "test-key", "api_base_url": ksrv.URL,
		"address_field": "road_addr", "lotno_field": "lotno_addr", "rps": 1000.0,
		"naver_client_id": "nid", "naver_client_secret": "nsecret", "naver_base_url": nsrv.URL,
	}); err != nil {
		t.Fatalf("init: %v", err)
	}

	rec, _ := s.Process(map[string]any{"road_addr": "강원특별자치도 춘천시 춘천로 22"})
	if kcalls.Load() == 0 {
		t.Error("카카오를 먼저 시도해야 함")
	}
	if ncalls.Load() == 0 {
		t.Error("카카오 실패 후 네이버 폴백을 호출해야 함")
	}
	if rec["geo_status"] != "ok" || rec["geo_source"] != "naver" {
		t.Errorf("네이버가 찾은 좌표로 채워져야 함: status=%v source=%v", rec["geo_status"], rec["geo_source"])
	}
	if rec["lat"] != 37.8668522 {
		t.Errorf("네이버 좌표: %v", rec["lat"])
	}
}

// 네이버 키가 없으면 폴백을 건너뛴다(기존 카카오 전용 동작 보존).
func TestProcess_NaverSkippedWithoutKey(t *testing.T) {
	ksrv, _ := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "", "", false, false
	})
	defer ksrv.Close()
	s := newTestStage(t, ksrv.URL, "test-key") // 네이버 키 미설정

	rec, _ := s.Process(map[string]any{"road_addr": "강원특별자치도 춘천시 춘천로 22"})
	if rec["geo_status"] != statusNotFound {
		t.Errorf("네이버 키 없으면 카카오 not_found 로 끝나야 함: %v", rec["geo_status"])
	}
}

func TestProcess_SuccessCacheHitAndAddressChange(t *testing.T) {
	srv, calls := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "37.5665", "126.9780", true, true
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	rec, err := s.Process(map[string]any{"road_addr": "서울특별시 중구 세종대로 110"})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if rec["lat"] != 37.5665 || rec["geo_match"] != "road" || rec["geo_status"] != "ok" {
		t.Errorf("attach wrong: %v", rec)
	}

	// 같은 주소 → 캐시 히트 (API 재호출 없음) — "이미 좌표 있으면 동작 안 함"
	before := calls.Load()
	rec2, _ := s.Process(map[string]any{"road_addr": "서울특별시 중구 세종대로 110"})
	if calls.Load() != before || rec2["lat"] != 37.5665 {
		t.Errorf("expected cache hit: calls %d→%d, %v", before, calls.Load(), rec2)
	}

	// 주소 변경 → 캐시 미스 → 재지오코딩
	_, _ = s.Process(map[string]any{"road_addr": "서울특별시 중구 세종대로 999"})
	if calls.Load() == before {
		t.Errorf("expected new API call for changed address")
	}
}

// skip_if_geocoded 게이트: CDC 재처리 시 이미 이 주소로 좌표가 채워진 레코드는
// 재지오코딩하지 않는다("주소 안 바뀜 AND 좌표 있음"). 무변경 UPDATE·자기write 루프 차단.
func TestProcess_SkipWhenAlreadyGeocoded(t *testing.T) {
	srv, calls := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "37.5665", "126.9780", true, true
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	// CDC after: 주소가 이미 geo_addr(같은 주소의 정규화값)로 지오코딩돼 lat 이 차 있음.
	// normalizeAddress("서울특별시 중구 세종대로 110") == "서울특별시 중구 세종대로 110"
	rec, _ := s.Process(map[string]any{
		"_cdc_type": "update",
		"road_addr": "서울특별시 중구 세종대로 110",
		"geo_addr":  "서울특별시 중구 세종대로 110",
		"lat":       37.5665, "lon": 126.9780,
	})
	if calls.Load() != 0 {
		t.Errorf("이미 지오코딩된 무변경 레코드는 API 를 부르면 안 됨: calls=%d", calls.Load())
	}
	// 무변경 → 드롭(nil). sink 로 통과시키면 CDC after 의 옛 lat 이 되쓰여 race 가 난다.
	if rec != nil {
		t.Errorf("무변경 레코드는 드롭(nil)돼야 함: %v", rec)
	}
}

// 주소는 그대로여도 좌표가 비어(lat 없음/nil) 들어오면 재지오코딩해 채운다.
// (이전 지오코딩 실패분, batch 가 좌표 없이 수집한 신규분 복구)
func TestProcess_GeocodeWhenCoordEmptyEvenIfAddressSame(t *testing.T) {
	srv, calls := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "37.5665", "126.9780", true, true
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	// geo_addr 은 같지만 lat 이 nil(빈 좌표) → 게이트 통과, 지오코딩 수행.
	rec, _ := s.Process(map[string]any{
		"_cdc_type": "update",
		"road_addr": "서울특별시 중구 세종대로 110",
		"geo_addr":  "서울특별시 중구 세종대로 110",
		"lat":       nil, "lon": nil,
	})
	if calls.Load() == 0 {
		t.Error("좌표가 비어 있으면 주소가 같아도 지오코딩해야 함")
	}
	if rec["lat"] != 37.5665 {
		t.Errorf("새 좌표로 채워져야 함: %v", rec["lat"])
	}
}

// 주소가 바뀌면(geo_addr != 현재 주소) 좌표가 있어도 재지오코딩한다.
func TestProcess_GeocodeWhenAddressChanged(t *testing.T) {
	srv, calls := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "35.1796", "129.0756", true, true
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	rec, _ := s.Process(map[string]any{
		"_cdc_type": "update",
		"road_addr": "부산광역시 중구 중앙대로 2", // 바뀐 주소
		"geo_addr":  "서울특별시 중구 세종대로 110", // 예전 주소로 지오코딩됨
		"lat":       37.5665, "lon": 126.9780,
	})
	if calls.Load() == 0 {
		t.Error("주소가 바뀌면 재지오코딩해야 함")
	}
	if rec["lat"] != 35.1796 {
		t.Errorf("새 좌표로 갱신돼야 함: %v", rec["lat"])
	}
}

// _cdc_type=delete 는 좌표 계산 없이 통과.
func TestProcess_DeletePassthrough(t *testing.T) {
	srv, calls := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "37.5", "127.0", true, true
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	rec, _ := s.Process(map[string]any{
		"_cdc_type": "delete",
		"road_addr": "서울특별시 중구 세종대로 110",
	})
	if calls.Load() != 0 {
		t.Errorf("delete 는 API 를 부르면 안 됨: calls=%d", calls.Load())
	}
	if _, ok := rec["lat"]; ok {
		t.Error("delete 는 좌표를 세팅하지 않아야 함")
	}
}

func TestProcess_CacheOnlyModeWithoutKey(t *testing.T) {
	srv, calls := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "37.5", "127.0", true, true
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "") // 키 없음 — 캐시 전용 모드

	rec, _ := s.Process(map[string]any{"road_addr": "서울특별시 중구 세종대로 110"})
	if calls.Load() != 0 {
		t.Errorf("no key must not call API")
	}
	if rec["geo_status"] != "no_api_key" {
		t.Errorf("expected no_api_key marker, got %v", rec["geo_status"])
	}
}

func TestProcess_UnfixableSkipsAPI(t *testing.T) {
	srv, calls := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "37.5", "127.0", true, true
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	rec, _ := s.Process(map[string]any{"road_addr": "경기도 성남시 수정구 태평인라인장 앞"})
	if calls.Load() != 0 {
		t.Errorf("unfixable must not reach API")
	}
	if rec["geo_status"] != "unfixable" {
		t.Errorf("expected unfixable marker, got %v", rec["geo_status"])
	}
	if _, ok := rec["lat"]; ok {
		t.Errorf("unfixable must not have coords")
	}
}

func TestProcess_LotnoFallback(t *testing.T) {
	srv, _ := mockKakao(t, func(q string) (string, string, bool, bool) {
		if q == "인천광역시 서구 가좌동 399" {
			return "37.49", "126.67", false, true
		}
		return "", "", false, false
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	rec, _ := s.Process(map[string]any{
		"road_addr":  "서울특별시 어딘가 이상한주소 1",
		"lotno_addr": "인천광역시 서구 가좌동 399",
	})
	if rec["lat"] != 37.49 || rec["geo_match"] != "region" {
		t.Errorf("lotno fallback failed: %v", rec)
	}
}

func TestProcess_OutOfBoundsRejected(t *testing.T) {
	srv, _ := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "35.6762", "139.6503", true, true // 도쿄 — 오매칭
	})
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	rec, _ := s.Process(map[string]any{"road_addr": "서울특별시 중구 세종대로 110"})
	if _, ok := rec["lat"]; ok {
		t.Errorf("out-of-bounds must be rejected: %v", rec)
	}
	if rec["geo_status"] != "not_found" {
		t.Errorf("expected not_found, got %v", rec["geo_status"])
	}
}

func TestProcess_Retry429(t *testing.T) {
	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"documents": []any{
			map[string]any{"x": "126.978", "y": "37.5665", "road_address": map[string]any{}},
		}})
	}))
	defer srv.Close()
	s := newTestStage(t, srv.URL, "test-key")

	rec, _ := s.Process(map[string]any{"road_addr": "서울특별시 중구 세종대로 110"})
	if rec["lat"] != 37.5665 || attempt != 2 {
		t.Errorf("expected success after 1 retry (attempts=%d): %v", attempt, rec)
	}
}

func TestProcess_NoAddressMarker(t *testing.T) {
	s := newTestStage(t, "http://unused", "test-key")
	rec, err := s.Process(map[string]any{"name": "화장실"})
	if err != nil || rec["geo_status"] != "no_address" {
		t.Errorf("expected no_address marker: %v %v", rec, err)
	}
}

func TestInit_Validation(t *testing.T) {
	if err := (&Stage{}).Init(map[string]any{}); err == nil {
		t.Error("expected error without address_field")
	}
}

// 원본 도로명 번지가 틀렸고 지번주소로는 찾히는 레코드 — 네이버 폴백이 도로명만 시도하면
// 놓친다. 실측: '서천군 서면 요포길 123'(원본, 실제 135번지)은 두 API 모두 실패하지만
// 지번 '서면 도둔리 1222-33' 으로는 네이버가 찾는다(좌표 없는 810건 중 348건이 지번 보유).
func TestProcess_NaverTriesLotnoCandidate(t *testing.T) {
	ksrv, _ := mockKakao(t, func(q string) (string, string, bool, bool) {
		return "", "", false, false // 카카오는 도로명·지번 모두 실패
	})
	defer ksrv.Close()

	var naverQueries []string
	nsrv, _ := mockNaver(t, func(q string) (string, string, bool, bool) {
		naverQueries = append(naverQueries, q)
		if strings.Contains(q, "도둔리") { // 지번으로만 찾힌다
			return "36.1566490", "126.5047516", true, true
		}
		return "", "", false, false
	})
	defer nsrv.Close()

	s := &Stage{}
	if err := s.Init(map[string]any{
		"api_key": "test-key", "api_base_url": ksrv.URL,
		"address_field": "road_addr", "lotno_field": "lotno_addr",
		"naver_client_id": "nid", "naver_client_secret": "nsecret", "naver_base_url": nsrv.URL,
		"rps": 1000,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	rec, err := s.Process(map[string]any{
		"road_addr":  "충청남도 서천군 서면 요포길 123",
		"lotno_addr": "충청남도 서천군 서면 도둔리 1222-33",
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if rec["geo_status"] != statusOK {
		t.Fatalf("geo_status = %v, want ok (네이버가 지번으로 찾아야 한다). naver 질의: %v",
			rec["geo_status"], naverQueries)
	}
	if rec["geo_source"] != "naver" {
		t.Errorf("geo_source = %v, want naver", rec["geo_source"])
	}

	// 지번 후보가 실제로 네이버에 전달됐는지 — 이게 없으면 이번 수정이 무의미하다.
	sawLotno := false
	for _, q := range naverQueries {
		if strings.Contains(q, "도둔리") {
			sawLotno = true
		}
	}
	if !sawLotno {
		t.Errorf("네이버에 지번 후보가 전달되지 않았다: %v", naverQueries)
	}
}

// 네이버는 동 이름만 줘도 좌표를 반환한다(동 중심점). 시설 위치가 아니므로 거부해야 한다 —
// "근사 좌표는 성공이 아니다" 원칙. addressElements 의 LAND_NUMBER/BUILDING_NUMBER 로 판정한다.
func TestNaverGeocode_RejectsRegionCentroid(t *testing.T) {
	// addressElements 에 번지가 없는 응답(동 레벨 매칭)을 직접 만든다.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "OK",
			"addresses": []any{map[string]any{
				"x": "126.9531", "y": "37.5065", "jibunAddress": "서울특별시 동작구 동작동",
				"addressElements": []any{
					map[string]any{"types": []string{"SIDO"}, "longName": "서울특별시"},
					map[string]any{"types": []string{"SIGUGUN"}, "longName": "동작구"},
					map[string]any{"types": []string{"DONGMYUN"}, "longName": "동작동"},
					map[string]any{"types": []string{"LAND_NUMBER"}, "longName": ""}, // 비어 있음
				},
			}},
		})
	}))
	defer srv.Close()

	s := &Stage{}
	if err := s.Init(map[string]any{
		"address_field": "a",
		"naver_client_id": "nid", "naver_client_secret": "nsecret", "naver_base_url": srv.URL,
		"rps": 1000,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if r := s.naverGeocode("서울특별시 동작구 동작동"); r != nil {
		t.Errorf("동 중심점을 성공으로 받았다: %+v", r)
	}
}

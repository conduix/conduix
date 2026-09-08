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
		"경기도 성남시 수정구 태평인라인장",             // 서술어(앞) 제거 후 — 건물명만, 번지 없음
		"경상북도 포항시 북구 송라면 조사리",            // 서술어(해안가) 제거 후 — 리 이름만, 번지 없음
		"강원특별자치도 횡성군 우천면 우항리 583-2외 8필지", // 외 N필지
		"경기도 평택시 고덕면 고덕로 283(좌교리",        // 안 닫힌 괄호(파손)
		"강원특별자치도 임계면 송계리",                // 리 이름만
		"경기도 고양시 일산동구",                   // 행정구역만
		// 번지 없는 도로명은 의도적으로 불가 — 카카오가 도로 전체 대표좌표(address_type=ROAD)를
		// 주고 후보가 10건씩 나와, 화장실이 아닌 위치가 박힌다.
		"강원특별자치도 강릉시 남부로",
		"강원특별자치도 강릉시 강동면 율곡로",
	}
	fixable := []string{
		"강원특별자치도 양양군 현남면 인구길 33", // 도로명+번지
		"인천광역시 서구 가좌동 399",       // 지번(동+번지)
		"가좌4동 399",               // sido 접두 없는 지번 — 형태만 맞으면 시도
		"남문로 352",                // sido 접두 없는 도로명 — 시도
		"전남광주통합특별시 곡성군 삼기면 곡순로 1436",
		"강원특별자치도 고성군 거진읍 거진항1길 4", // '인근' 제거 후 번지 남음
		// 아래는 2026-09-08 실측으로 드러난 누락 패턴 — 전부 카카오 API 가 찾는 주소인데
		// reAddressShape 가 걸러내 API 호출조차 하지 않았다(실패 3,157건의 대부분).
		"강원특별자치도 동해시 공단 7로 31",        // 로 앞이 숫자(원본에 공백 섞임, 실제 '공단7로')
		"강원특별자치도 정선군 정선읍 녹송 8길 55",    // 길 앞이 숫자
		"강원특별자치도 정선군 여량면 여량 3리 649-1", // 리 앞이 숫자
		"강원특별자치도 동해시 구호동239",          // 동+번지 공백 없음
		"강원특별자치도 동해시 달방동 산 3-6",       // 산번지(최대 누락 덩어리)
		"경기도 광명시 가학동 산14-1",           // 산번지 공백 없음
		"강원특별자치도 고성군 현내면 757-5",       // 동/리 없이 번지만
		// 지하상가 주소 — 로/길 뒤에 '지하' 가 끼어 번호가 바로 오지 않아 탈락했다(142건).
		// API 는 '지하' 포함 여부와 무관하게 같은 좌표를 준다(실측).
		"서울특별시 영등포구 경인로 지하 843",
		"서울특별시 영등포구 국회대로 지하758",
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
		"road_addr": "부산광역시 중구 중앙대로 2",   // 바뀐 주소
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
		"address_field":   "a",
		"naver_client_id": "nid", "naver_client_secret": "nsecret", "naver_base_url": srv.URL,
		"rps": 1000,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	if r := s.naverGeocode("서울특별시 동작구 동작동"); r != nil {
		t.Errorf("동 중심점을 성공으로 받았다: %+v", r)
	}
}

// 원본 주소가 틀렸을 때 보정 주소(override)로 지오코딩해야 한다.
// 원본은 수집 파이프라인이 매번 덮어쓰므로 오타를 고쳐도 원복된다(실측: 창원시 표기
// 정정 95건이 배치 재실행으로 되돌아갔다). 보정 컬럼을 수집 sink 의 columns 에서
// 빼두면 upsert 가 건드리지 않아 사람이 고친 값이 살아남는다.
func TestProcess_OverrideAddressWins(t *testing.T) {
	var queried []string
	srv, _ := mockKakao(t, func(q string) (string, string, bool, bool) {
		queried = append(queried, q)
		if strings.Contains(q, "제2부두로") { // 보정된 주소만 찾힌다
			return "35.1948133", "128.5732184", true, true
		}
		return "", "", false, false
	})
	defer srv.Close()

	s := &Stage{}
	if err := s.Init(map[string]any{
		"api_key": "test-key", "api_base_url": srv.URL,
		"address_field": "road_addr", "override_field": "geo_addr_override",
		"rps": 1000,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	rec, err := s.Process(map[string]any{
		"road_addr":         "경상남도마산합포구제2두부로30", // 원본(오타: 두부로, 창원시 누락)
		"geo_addr_override": "경상남도 창원시 마산합포구 제2부두로 30",
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if rec["geo_status"] != statusOK {
		t.Fatalf("geo_status = %v, want ok. 질의: %v", rec["geo_status"], queried)
	}
	// geo_addr 앵커도 보정 주소 기준이어야 한다 — 원본 기준이면 다음 CDC 에서
	// 주소가 바뀐 것으로 오판해 매번 재지오코딩한다.
	if got, _ := rec["geo_addr"].(string); !strings.Contains(got, "제2부두로") {
		t.Errorf("geo_addr = %q, want 보정 주소 기준", got)
	}
}

// 보정 컬럼이 비어 있으면(대다수 레코드) 원본을 그대로 쓴다 — 설정만 켜도 무해해야 한다.
func TestProcess_OverrideEmptyFallsBackToOriginal(t *testing.T) {
	srv, _ := mockKakao(t, func(q string) (string, string, bool, bool) {
		if strings.Contains(q, "삼양로") {
			return "37.6103271", "127.0227433", true, true
		}
		return "", "", false, false
	})
	defer srv.Close()

	s := &Stage{}
	if err := s.Init(map[string]any{
		"api_key": "test-key", "api_base_url": srv.URL,
		"address_field": "road_addr", "override_field": "geo_addr_override",
		"rps": 1000,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	for _, ov := range []any{nil, "", "   "} { // 없음·빈문자·공백 모두 미사용
		rec, err := s.Process(map[string]any{
			"road_addr": "서울특별시 성북구 삼양로 78", "geo_addr_override": ov,
		})
		if err != nil {
			t.Fatalf("Process(%v): %v", ov, err)
		}
		if rec["geo_status"] != statusOK {
			t.Errorf("override=%v → geo_status = %v, want ok", ov, rec["geo_status"])
		}
	}
}

// 시도명이 오타면 접두를 고치는 게 아니라 떼야 한다 — API 가 시군 이름으로 시도를 판별한다.
// '경상님도→경상남도' 같은 오타 사전은 틀린 답을 준다: '경상붓도 봉화군' 의 봉화군은
// 경상'북'도라서, 남도로 고치면 여전히 실패한다. 제거가 옳다(실측).
func TestSidoStrippedVariant(t *testing.T) {
	cases := []struct{ in, want string }{
		// 오타 접두 → 제거된 후보 생성
		{"경상님도 양산시 황산로 719", "양산시 황산로 719"},
		{"경상붓도 봉화군 소천면 고선리 산5-1", "봉화군 소천면 고선리 산5-1"},
		// 정상 접두 → 변형 없음(불필요한 후보로 API 낭비하지 않는다)
		{"경상남도 양산시 황산로 719", ""},
		{"서울특별시 성북구 삼양로 78", ""},
		{"강원특별자치도 동해시 구호동239", ""},
		// 시도 형태가 아니거나 뒤에 시/군/구가 없으면 변형 없음
		{"양산시 황산로 719", ""},  // 이미 시부터 시작
		{"태평인라인장", ""},       // 공백 없음
		{"경상님도 어딘가로 12", ""}, // 뒤가 시/군/구 아님 → 판별 근거 없음
	}
	for _, tc := range cases {
		if got := sidoStrippedVariant(tc.in); got != tc.want {
			t.Errorf("sidoStrippedVariant(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// 오타 시도 주소가 실제로 지오코딩되는지 — spacingVariants 후보에 실려 API 로 가야 한다.
func TestProcess_TypoSidoRecovered(t *testing.T) {
	var queried []string
	srv, _ := mockKakao(t, func(q string) (string, string, bool, bool) {
		queried = append(queried, q)
		if q == "양산시 황산로 719" { // 접두를 뗀 후보만 찾힌다
			return "35.3050", "129.0090", true, true
		}
		return "", "", false, false
	})
	defer srv.Close()

	s := &Stage{}
	if err := s.Init(map[string]any{
		"api_key": "test-key", "api_base_url": srv.URL,
		"address_field": "road_addr", "rps": 1000,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	rec, err := s.Process(map[string]any{"road_addr": "경상님도 양산시 황산로 719"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if rec["geo_status"] != statusOK {
		t.Fatalf("geo_status = %v, want ok. 질의: %v", rec["geo_status"], queried)
	}
}

// 시설명 폴백의 오매칭 차단 검증. 표본 14건 실측으로 임계(0.7)를 정했고, 그 경계가
// 유지되는지 고정한다 — 낮추면 오매칭이 통과하고 높이면 정상 케이스를 놓친다.
func TestPrefixMatchRatio_SeparatesMismatches(t *testing.T) {
	cases := []struct {
		orig, got string
		accept    bool
	}{
		// 정상: 뒤에 수식어가 붙는 형태 → 접두가 일치한다
		{"국립서울현충원 호국전시관", "국립서울현충원 호국전시관", true},
		{"국립서울현충원 유공자", "국립서울현충원 독립유공자묘역", true}, // 0.70 — 경계값
		{"중부대학교", "중부대학교 국제캠퍼스", true},
		{"조사리 간이", "조사리간이해수욕장", true},
		{"검단(서울)졸음쉼터", "검단졸음쉼터 서울방향", true}, // 괄호 정규화가 없으면 0.25 로 탈락
		{"도원 어린이공원", "도원어린이공원", true},
		// 오매칭: 고유명 자체가 다르다 → 접두가 갈린다
		{"현내리마을회관", "수통1리마을회관", false}, // 0.00
		{"한국의원", "한국흉부외과의원", false},    // 0.50
		{"장성 주유소", "장성우리주유소", false},   // 0.40
	}
	for _, tc := range cases {
		a, b := normalizePlaceName(tc.orig), normalizePlaceName(tc.got)
		r := prefixMatchRatio(a, b)
		got := r >= placeNameMinRatio
		if got != tc.accept {
			t.Errorf("%q vs %q: ratio=%.2f → %v, want %v",
				tc.orig, tc.got, r, got, tc.accept)
		}
	}
}

// 이름이 너무 짧으면 우연히 접두가 맞아 오매칭한다('시장' → '정선아리랑시장' 은 0.0 이지만
// '시장' → '시장앞공원' 같은 케이스는 1.0 이 되어버린다). 3자 미만은 아예 시도하지 않는다.
func TestPlaceNameFallback_RejectsShortNames(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = json.NewEncoder(w).Encode(map[string]any{"documents": []any{}})
	}))
	defer srv.Close()

	s := &Stage{}
	if err := s.Init(map[string]any{
		"api_key": "test-key", "api_base_url": srv.URL,
		"address_field": "a", "place_name_field": "nm", "rps": 1000,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if r := s.placeNameFallback("시장", "서울특별시 강남구 어딘가로 1"); r != nil {
		t.Errorf("짧은 이름을 채택했다: %+v", r)
	}
	if called {
		t.Error("짧은 이름인데 API 를 호출했다 — 쿼터 낭비")
	}
}

// 지역 한정 없이 검색하면 동명 시설 오매칭 위험이 크다('성주빌딩' 단독 검색은 89건).
func TestPlaceNameFallback_RequiresRegion(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = json.NewEncoder(w).Encode(map[string]any{"documents": []any{}})
	}))
	defer srv.Close()

	s := &Stage{}
	if err := s.Init(map[string]any{
		"api_key": "test-key", "api_base_url": srv.URL,
		"address_field": "a", "place_name_field": "nm", "rps": 1000,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// 시도·시군구가 없는 주소 → 검색 범위를 좁힐 수 없으므로 시도하지 않는다
	if r := s.placeNameFallback("성주빌딩", "태평인라인장"); r != nil {
		t.Errorf("지역 한정 없이 채택했다: %+v", r)
	}
	if called {
		t.Error("지역 정보가 없는데 API 를 호출했다")
	}
}

// 주소로 실패한 뒤 시설명으로 구제되는 전체 경로.
func TestProcess_PlaceNameFallbackRecovers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("query")
		if strings.Contains(r.URL.Path, "keyword") && strings.Contains(q, "성주빌딩") {
			_ = json.NewEncoder(w).Encode(map[string]any{"documents": []any{map[string]any{
				"place_name": "성주빌딩", "address_name": "경남 창원시 성산구 성주동 127",
				"road_address_name": "경남 창원시 성산구 삼정자로43번길 8",
				"x":                 "128.710953986569", "y": "35.1986927086103",
			}}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"documents": []any{}}) // 주소검색은 실패
	}))
	defer srv.Close()

	s := &Stage{}
	if err := s.Init(map[string]any{
		"api_key": "test-key", "api_base_url": srv.URL,
		"address_field": "road_addr", "place_name_field": "rstrm_nm", "rps": 1000,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	rec, err := s.Process(map[string]any{
		"road_addr": "경상남도 창원시 성산구 삼정자로48번길 3", // 원본(번지 오기)
		"rstrm_nm":  "성주빌딩 화장실",
	})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if rec["geo_status"] != statusOK {
		t.Fatalf("geo_status = %v, want ok", rec["geo_status"])
	}
	if rec["geo_source"] != "kakao_place" {
		t.Errorf("geo_source = %v, want kakao_place (출처 구분 필요)", rec["geo_source"])
	}
}

// 시군구만 대조하면 같은 군 안의 다른 읍/면을 통과시킨다.
// 실측 오매칭: '금강삼사' 원본 '고성군 현내면 화포리 561-1' → 반환 '고성군 거진읍 화진포길 204-25'.
// 같은 고성군이고 시설명도 정확히 일치해 접두·시군구 검증을 모두 통과했다.
func TestPlaceNameFallback_RejectsDifferentEupMyeon(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"documents": []any{map[string]any{
			"place_name": "금강삼사", "address_name": "강원특별자치도 고성군 거진읍 화진포리 산1",
			"road_address_name": "강원특별자치도 고성군 거진읍 화진포길 204-25",
			"x":                 "128.4464014", "y": "38.4662009",
		}}})
	}))
	defer srv.Close()

	s := &Stage{}
	if err := s.Init(map[string]any{
		"api_key": "test-key", "api_base_url": srv.URL,
		"address_field": "a", "place_name_field": "nm", "rps": 1000,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// 원본은 현내면, 반환은 거진읍 → 거부해야 한다
	if r := s.placeNameFallback("금강삼사", "강원특별자치도 고성군 현내면 화포리 561-1"); r != nil {
		t.Errorf("다른 읍/면을 채택했다: %+v", r)
	}
	// 같은 읍/면이면 채택
	if r := s.placeNameFallback("금강삼사", "강원특별자치도 고성군 거진읍 화진포리 1"); r == nil {
		t.Error("같은 읍/면인데 거부했다")
	}
}

// 원본에 읍/면/동이 없으면(시군구까지만 기재) 이 검증은 건너뛴다 — 대조 근거가 없고,
// 그런 레코드는 주소검색이 애초에 불가해 시설명이 유일한 단서다.
// 실측: '천병약수터' 원본 '서울특별시 노원구' → 시설명으로 '중계동 산 101-1' 정확히 찾음.
func TestPlaceNameFallback_SkipsEmdCheckWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"documents": []any{map[string]any{
			"place_name": "천병약수터", "address_name": "서울 노원구 중계동 산 101-1",
			"road_address_name": "", "x": "127.0867838", "y": "37.6550731",
		}}})
	}))
	defer srv.Close()

	s := &Stage{}
	if err := s.Init(map[string]any{
		"api_key": "test-key", "api_base_url": srv.URL,
		"address_field": "a", "place_name_field": "nm", "rps": 1000,
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if r := s.placeNameFallback("천병약수터", "서울특별시 노원구"); r == nil {
		t.Error("원본에 읍면동이 없는데 거부했다 — 이 경로가 시설명 폴백의 핵심 용도다")
	}
}

func TestEupMyeonDongOf(t *testing.T) {
	cases := map[string]string{
		"강원특별자치도 고성군 현내면 화포리 561-1": "현내면",
		"서울특별시 노원구 중계동 산 101-1":     "중계동",
		"서울특별시 중랑구 면목3,8동 27":       "면목3,8동", // 숫자 붙은 행정동
		"경상남도 창원시 의창구 중앙대로 181":     "",       // 도로명만 — 읍면동 없음
		"서울특별시 노원구":                 "",       // 시군구까지만
		// '리' 는 뽑지 않는다 — 카카오 도로명 주소에 리가 없어 대조하면 정상 매칭도 탈락한다
		"전북특별자치도 순창군 쌍치면 둔전리": "쌍치면",
	}
	for in, want := range cases {
		if got := eupMyeonDongOf(in); got != want {
			t.Errorf("eupMyeonDongOf(%q) = %q, want %q", in, got, want)
		}
	}
}

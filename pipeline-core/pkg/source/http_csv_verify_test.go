package source

import (
	"context"
	"os"
	"testing"
	"unicode/utf8"

	"github.com/conduix/conduix/pipeline-core/pkg/config"
)

// 실제 공공데이터 CSV 다운로드 URL(localdata.go.kr)로 검증한다.
// 인증키 없이 받아지지만 Referer 가 없으면 403 이고, 서버는 charset=UTF-8 이라
// 응답하면서 실제로는 cp949 를 준다.
func TestHTTPSource_RealPublicDataCSV(t *testing.T) {
	// 외부 배포처에 실제로 붙는 테스트다. CI 에서는 네트워크·차단 정책에 따라
	// 불안정하므로 기본적으로 건너뛴다. 로컬 검증은 RUN_NETWORK_TESTS=1 로 켠다.
	if os.Getenv("RUN_NETWORK_TESTS") == "" {
		t.Skip("네트워크 테스트 — RUN_NETWORK_TESTS=1 로 실행")
	}
	cfg := config.SourceV2{
		Type:     "rest_api",
		URL:      "https://file.localdata.go.kr/file/download/public_restroom_info/info",
		Method:   "GET",
		Format:   "csv",
		Encoding: "cp949",
		Headers: map[string]string{
			"Referer":    "https://www.localdata.go.kr/",
			"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/151.0.0.0 Safari/537.36",
		},
	}
	src, err := NewHTTPSource(cfg)
	if err != nil {
		t.Fatalf("NewHTTPSource: %v", err)
	}
	src.client.Timeout = 180 * 1e9 // 16MB 다운로드

	recs, errs := src.Read(context.Background())
	n, valid := 0, 0
	var sample map[string]any
	for r := range recs {
		n++
		if n == 1 {
			sample = r.Data
		}
		if v, ok := r.Data["화장실명"].(string); ok && v != "" && utf8.ValidString(v) {
			valid++
		}
	}
	if e := <-errs; e != nil {
		t.Fatalf("read: %v", e)
	}
	t.Logf("레코드 %d건, 한글 유효 %d건", n, valid)
	if sample != nil {
		t.Logf("  첫 레코드: 화장실명=%q 관리번호=%q", sample["화장실명"], sample["관리번호"])
		t.Logf("  CSV 전용 컬럼: 남성용-장애인용대변기수=%q 기저귀교환대장소=%q",
			sample["남성용-장애인용대변기수"], sample["기저귀교환대장소"])
	}
	if n < 50000 {
		t.Errorf("레코드 %d건 — 전량(약 53,582)이 아니다", n)
	}
	if valid == 0 {
		t.Error("한글이 하나도 안 읽혔다 — 인코딩 처리 실패")
	}
}

package source

import (
	"strings"
	"testing"
)

// 국내 공공데이터 CSV 는 대부분 cp949 다. 지정하지 않으면 한글이 깨진 채 적재된다.
//
// euc-kr 이 아니라 cp949 를 쓰는 이유: euc-kr 디코더는 확장 한글(똠, 뷁 등)에서
// 변환이 끊긴다. 실측에서 같은 파일이 53,775행 → 4,722행으로 잘렸다.
// Go 의 korean.EUCKR 은 CP949(EUC-KR 확장)를 구현하므로 둘 다 안전하게 읽는다.
func TestDecodedReader(t *testing.T) {
	// "한글" 의 cp949 바이트
	cp949 := []byte{0xC7, 0xD1, 0xB1, 0xDB}

	for _, enc := range []string{"cp949", "euc-kr", "EUC-KR", "windows-949"} {
		r, err := decodedReader(strings.NewReader(string(cp949)), enc)
		if err != nil {
			t.Fatalf("enc=%q: %v", enc, err)
		}
		buf := new(strings.Builder)
		if _, err := copyAll(buf, r); err != nil {
			t.Fatalf("enc=%q read: %v", enc, err)
		}
		if got := buf.String(); got != "한글" {
			t.Errorf("enc=%q → %q, want %q", enc, got, "한글")
		}
	}
}

// utf-8(기본)은 원본을 그대로 흘려보낸다 — 불필요한 변환을 끼우지 않는다.
func TestDecodedReader_UTF8Passthrough(t *testing.T) {
	for _, enc := range []string{"", "utf-8", "UTF8"} {
		r, err := decodedReader(strings.NewReader("한글"), enc)
		if err != nil {
			t.Fatalf("enc=%q: %v", enc, err)
		}
		buf := new(strings.Builder)
		if _, err := copyAll(buf, r); err != nil {
			t.Fatal(err)
		}
		if buf.String() != "한글" {
			t.Errorf("enc=%q → %q", enc, buf.String())
		}
	}
}

// 모르는 인코딩은 생성 시점에 거부한다 — 응답을 받은 뒤 실패하면
// 16MB 를 내려받고 버리는 셈이고 어디서 깨졌는지도 알기 어렵다.
func TestDecodedReader_RejectsUnknown(t *testing.T) {
	if _, err := decodedReader(strings.NewReader(""), "shift-jis"); err == nil {
		t.Error("모르는 인코딩이 통과했다")
	}
}

func copyAll(dst *strings.Builder, src interface{ Read([]byte) (int, error) }) (int, error) {
	b := make([]byte, 512)
	total := 0
	for {
		n, err := src.Read(b)
		if n > 0 {
			dst.Write(b[:n])
			total += n
		}
		if err != nil {
			if err.Error() == "EOF" {
				return total, nil
			}
			return total, err
		}
	}
}

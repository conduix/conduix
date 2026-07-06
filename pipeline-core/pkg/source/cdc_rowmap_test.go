package source

import (
	"testing"

	"github.com/go-mysql-org/go-mysql/schema"
)

// 텍스트 컬럼([]byte)은 string 으로, 바이너리 컬럼(TYPE_BINARY)은 원본 []byte 로 유지해야 한다.
// (바이너리를 string 강제하면 비UTF-8 바이트가 손상된다.)
func TestRowToMap_BinaryVsText(t *testing.T) {
	cols := []schema.TableColumn{
		{Name: "id", Type: schema.TYPE_NUMBER},
		{Name: "name", Type: schema.TYPE_STRING},     // 텍스트
		{Name: "blob_col", Type: schema.TYPE_BINARY}, // 바이너리
	}
	binary := []byte{0x00, 0xff, 0x10, 0x80} // 비UTF-8
	row := []any{int64(1), []byte("hello"), binary}

	m := rowToMap(cols, row)

	if m["id"] != int64(1) {
		t.Errorf("id = %v, want 1", m["id"])
	}
	// 텍스트: string 변환
	if s, ok := m["name"].(string); !ok || s != "hello" {
		t.Errorf("name = %#v, want string \"hello\"", m["name"])
	}
	// 바이너리: []byte 유지(손상 없음)
	b, ok := m["blob_col"].([]byte)
	if !ok {
		t.Fatalf("blob_col = %T, want []byte (binary must not be string-forced)", m["blob_col"])
	}
	if len(b) != len(binary) {
		t.Fatalf("blob_col len = %d, want %d", len(b), len(binary))
	}
	for i := range binary {
		if b[i] != binary[i] {
			t.Errorf("blob_col[%d] = %#x, want %#x (binary corrupted)", i, b[i], binary[i])
		}
	}
}

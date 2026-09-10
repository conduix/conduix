package handlers

import (
	"encoding/json"
	"testing"
)

// 실행이 "No target cluster: ...mark a cluster as default" 로 막히는데, 그 조치를 수행할
// API 필드가 없어 DB 를 직접 고쳐야 했다 — 메시지는 해결책을 알려주지만 실행할 수단이 없었다.
func TestUpdateClusterRequest_AcceptsIsDefault(t *testing.T) {
	var req UpdateClusterRequest
	if err := json.Unmarshal([]byte(`{"is_default":true}`), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.IsDefault == nil {
		t.Fatal("is_default 가 구조체에 없으면 요청이 조용히 무시된다")
	}
	if !*req.IsDefault {
		t.Errorf("is_default = %v, want true", *req.IsDefault)
	}
}

// 포인터여야 "false 로 해제" 와 "미지정(기존값 유지)" 이 구분된다.
// 값 타입이면 다른 필드만 수정하는 요청이 default 를 조용히 해제한다.
func TestUpdateClusterRequest_DistinguishesUnsetFromFalse(t *testing.T) {
	var unset UpdateClusterRequest
	if err := json.Unmarshal([]byte(`{"name":"c1"}`), &unset); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if unset.IsDefault != nil {
		t.Error("미지정인데 값이 들어왔다 — 이름만 바꾸는 요청이 default 를 해제한다")
	}

	var explicit UpdateClusterRequest
	if err := json.Unmarshal([]byte(`{"is_default":false}`), &explicit); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if explicit.IsDefault == nil || *explicit.IsDefault {
		t.Error("false 로 명시한 해제가 전달되지 않았다")
	}
}

package foo

import "github.com/example/foo/sub"

// Marker 는 자기참조 import 를 거쳐 버전 표식을 돌려준다.
// 이 간접 참조가 핵심이다 — 복사본의 자기참조를 재작성하지 않으면
// 두 fork 의 Marker() 가 같은 sub 패키지를 보게 돼 값이 섞인다.
func Marker() string { return sub.Marker() }

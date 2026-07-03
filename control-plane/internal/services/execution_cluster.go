package services

import (
	"errors"

	"gorm.io/gorm"

	"github.com/conduix/conduix/control-plane/pkg/models"
)

// ErrNoExecutionCluster는 실행 대상 cluster를 확정할 수 없을 때 반환된다(그룹 없이는 실행 불가).
var ErrNoExecutionCluster = errors.New("no execution cluster")

// ResolveExecutionCluster는 실행 시점의 대상 cluster를 확정한다.
// 우선순위: 워크플로우 지정값 → default cluster → 실패(ErrNoExecutionCluster).
// 워크플로우가 cluster를 지정했으면 그 값을 신뢰한다(존재 검증은 배치 위임/agent 측 라우팅에 위임).
//
// 이 정책은 즉시 실행(StartWorkflow)과 수동 트리거(TriggerNow) 양쪽에서 동일하게 쓰인다.
// 한쪽만 cluster를 확정하면 발행 채널(cluster:<id>:execute)이 어긋나 agent가 메시지를 못 받는다.
func ResolveExecutionCluster(tx *gorm.DB, workflowClusterID string) (string, error) {
	if workflowClusterID != "" {
		return workflowClusterID, nil
	}
	var def models.Cluster
	if err := tx.Where("is_default = ?", true).First(&def).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrNoExecutionCluster
		}
		return "", err
	}
	return def.ID, nil
}

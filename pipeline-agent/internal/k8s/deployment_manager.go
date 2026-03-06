package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/conduix/conduix/shared/types"
)

// DeploymentManager K8s Deployment 관리자 (스트리밍 파이프라인용)
type DeploymentManager struct {
	client          *Client
	controlPlaneURL string
	runnerImage     string
}

// NewDeploymentManager DeploymentManager 생성
func NewDeploymentManager(client *Client, controlPlaneURL, runnerImage string) *DeploymentManager {
	return &DeploymentManager{
		client:          client,
		controlPlaneURL: controlPlaneURL,
		runnerImage:     runnerImage,
	}
}

// DeploymentSpec 스트리밍 Deployment 생성용 파라미터
type DeploymentSpec struct {
	WorkflowID      string
	PipelinesConfig string // JSON
	Replicas        int32
	Image           string // 커스텀 이미지 (플러그인용)
	Resources       *types.ResourceRequirements
	Namespace       string
	ServiceAccount  string
	NodeSelector    map[string]string
}

// CreateDeployment 스트리밍 파이프라인용 Deployment 생성
func (m *DeploymentManager) CreateDeployment(ctx context.Context, spec *DeploymentSpec) (*appsv1.Deployment, error) {
	deployName := fmt.Sprintf("conduix-stream-%s", sanitizeName(spec.WorkflowID))

	image := m.runnerImage
	if spec.Image != "" {
		image = spec.Image
	}
	if image == "" {
		return nil, fmt.Errorf("runner image is required")
	}

	namespace := spec.Namespace
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	replicas := spec.Replicas
	if replicas <= 0 {
		replicas = 1
	}

	labels := map[string]string{
		"app.kubernetes.io/name":       "conduix-runner",
		"app.kubernetes.io/component":  "streaming",
		"app.kubernetes.io/managed-by": "conduix-agent",
		"conduix.io/workflow-id":       sanitizeLabel(spec.WorkflowID),
	}

	envVars := []corev1.EnvVar{
		{Name: "EXECUTION_MODE", Value: "streaming"},
		{Name: "WORKFLOW_ID", Value: spec.WorkflowID},
		{Name: "PIPELINES_CONFIG", Value: spec.PipelinesConfig},
		{Name: "CONTROL_PLANE_URL", Value: m.controlPlaneURL},
	}

	resources := buildDeploymentResources(spec.Resources)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"conduix.io/workflow-id": sanitizeLabel(spec.WorkflowID),
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:      "pipeline-runner",
							Image:     image,
							Env:       envVars,
							Resources: resources,
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt32(8082),
									},
								},
								InitialDelaySeconds: 15,
								PeriodSeconds:       10,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt32(8082),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       5,
							},
						},
					},
				},
			},
		},
	}

	if len(spec.NodeSelector) > 0 {
		deployment.Spec.Template.Spec.NodeSelector = spec.NodeSelector
	}
	if spec.ServiceAccount != "" {
		deployment.Spec.Template.Spec.ServiceAccountName = spec.ServiceAccount
	}

	created, err := m.client.Clientset().AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment %s: %w", deployName, err)
	}

	return created, nil
}

// UpdateDeployment Deployment 업데이트 (이미지, replicas 등)
func (m *DeploymentManager) UpdateDeployment(ctx context.Context, namespace, name string, spec *DeploymentSpec) (*appsv1.Deployment, error) {
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	deployment, err := m.client.Clientset().AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s: %w", name, err)
	}

	if spec.Replicas > 0 {
		deployment.Spec.Replicas = &spec.Replicas
	}

	if spec.Image != "" {
		for i := range deployment.Spec.Template.Spec.Containers {
			if deployment.Spec.Template.Spec.Containers[i].Name == "pipeline-runner" {
				deployment.Spec.Template.Spec.Containers[i].Image = spec.Image
			}
		}
	}

	if spec.PipelinesConfig != "" {
		for i, env := range deployment.Spec.Template.Spec.Containers[0].Env {
			if env.Name == "PIPELINES_CONFIG" {
				deployment.Spec.Template.Spec.Containers[0].Env[i].Value = spec.PipelinesConfig
			}
		}
	}

	updated, err := m.client.Clientset().AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to update deployment %s: %w", name, err)
	}

	return updated, nil
}

// ScaleDeployment Deployment 레플리카 수 변경
func (m *DeploymentManager) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	deployment, err := m.client.Clientset().AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get deployment %s: %w", name, err)
	}

	deployment.Spec.Replicas = &replicas

	_, err = m.client.Clientset().AppsV1().Deployments(namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to scale deployment %s: %w", name, err)
	}

	return nil
}

// DeleteDeployment Deployment 삭제
func (m *DeploymentManager) DeleteDeployment(ctx context.Context, namespace, name string) error {
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	propagation := metav1.DeletePropagationForeground
	err := m.client.Clientset().AppsV1().Deployments(namespace).Delete(ctx, name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete deployment %s: %w", name, err)
	}
	return nil
}

// DeploymentStatus Deployment 상태 정보
type DeploymentStatus struct {
	Name              string `json:"name"`
	Namespace         string `json:"namespace"`
	Replicas          int32  `json:"replicas"`
	ReadyReplicas     int32  `json:"ready_replicas"`
	AvailableReplicas int32  `json:"available_replicas"`
	UpdatedReplicas   int32  `json:"updated_replicas"`
	Status            string `json:"status"` // running, progressing, degraded
}

// GetDeploymentStatus Deployment 상태 조회
func (m *DeploymentManager) GetDeploymentStatus(ctx context.Context, namespace, name string) (*DeploymentStatus, error) {
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	deploy, err := m.client.Clientset().AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment %s: %w", name, err)
	}

	status := &DeploymentStatus{
		Name:              deploy.Name,
		Namespace:         deploy.Namespace,
		Replicas:          deploy.Status.Replicas,
		ReadyReplicas:     deploy.Status.ReadyReplicas,
		AvailableReplicas: deploy.Status.AvailableReplicas,
		UpdatedReplicas:   deploy.Status.UpdatedReplicas,
	}

	desired := int32(1)
	if deploy.Spec.Replicas != nil {
		desired = *deploy.Spec.Replicas
	}

	switch {
	case deploy.Status.AvailableReplicas == desired:
		status.Status = "running"
	case deploy.Status.AvailableReplicas > 0:
		status.Status = "degraded"
	default:
		status.Status = "progressing"
	}

	return status, nil
}

// ListDeployments conduix가 관리하는 Deployment 목록 조회
func (m *DeploymentManager) ListDeployments(ctx context.Context, namespace string) ([]DeploymentStatus, error) {
	if namespace == "" {
		namespace = m.client.Namespace()
	}

	deployments, err := m.client.Clientset().AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app.kubernetes.io/managed-by=conduix-agent",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}

	var result []DeploymentStatus
	for _, deploy := range deployments.Items {
		desired := int32(1)
		if deploy.Spec.Replicas != nil {
			desired = *deploy.Spec.Replicas
		}

		s := DeploymentStatus{
			Name:              deploy.Name,
			Namespace:         deploy.Namespace,
			Replicas:          deploy.Status.Replicas,
			ReadyReplicas:     deploy.Status.ReadyReplicas,
			AvailableReplicas: deploy.Status.AvailableReplicas,
			UpdatedReplicas:   deploy.Status.UpdatedReplicas,
		}

		switch {
		case deploy.Status.AvailableReplicas == desired:
			s.Status = "running"
		case deploy.Status.AvailableReplicas > 0:
			s.Status = "degraded"
		default:
			s.Status = "progressing"
		}

		result = append(result, s)
	}

	return result, nil
}

// buildDeploymentResources types.ResourceRequirements를 K8s 포맷으로 변환
func buildDeploymentResources(res *types.ResourceRequirements) corev1.ResourceRequirements {
	k8sRes := corev1.ResourceRequirements{}
	if res == nil {
		return k8sRes
	}

	if res.Requests.CPU != "" || res.Requests.Memory != "" {
		k8sRes.Requests = corev1.ResourceList{}
		if res.Requests.CPU != "" {
			k8sRes.Requests[corev1.ResourceCPU] = resource.MustParse(res.Requests.CPU)
		}
		if res.Requests.Memory != "" {
			k8sRes.Requests[corev1.ResourceMemory] = resource.MustParse(res.Requests.Memory)
		}
	}

	if res.Limits.CPU != "" || res.Limits.Memory != "" {
		k8sRes.Limits = corev1.ResourceList{}
		if res.Limits.CPU != "" {
			k8sRes.Limits[corev1.ResourceCPU] = resource.MustParse(res.Limits.CPU)
		}
		if res.Limits.Memory != "" {
			k8sRes.Limits[corev1.ResourceMemory] = resource.MustParse(res.Limits.Memory)
		}
	}

	return k8sRes
}

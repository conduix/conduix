package k8s

import (
	"fmt"
	"os"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client K8s API 클라이언트 래퍼
type Client struct {
	clientset kubernetes.Interface
	namespace string
	config    *rest.Config
}

// NewClient K8s 클라이언트 생성
// InCluster 환경이면 자동 감지, 아니면 kubeconfig 사용
func NewClient(namespace string) (*Client, error) {
	if namespace == "" {
		namespace = getNamespace()
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		// InCluster가 아니면 kubeconfig에서 로드
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			home, _ := os.UserHomeDir()
			kubeconfig = home + "/.kube/config"
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create k8s config: %w", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s clientset: %w", err)
	}

	return &Client{
		clientset: clientset,
		namespace: namespace,
		config:    config,
	}, nil
}

// NewClientWithInterface 테스트용 클라이언트 생성
func NewClientWithInterface(clientset kubernetes.Interface, namespace string) *Client {
	return &Client{
		clientset: clientset,
		namespace: namespace,
	}
}

// Clientset K8s clientset 반환
func (c *Client) Clientset() kubernetes.Interface {
	return c.clientset
}

// Namespace 현재 네임스페이스 반환
func (c *Client) Namespace() string {
	return c.namespace
}

// getNamespace Pod가 실행 중인 네임스페이스 조회
func getNamespace() string {
	// ServiceAccount에서 네임스페이스 읽기
	if data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		return string(data)
	}
	// 환경변수에서 읽기
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}
	return "conduix"
}

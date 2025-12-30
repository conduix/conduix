# Conduix Helm Chart 배포 가이드

## 개요

이 Helm chart는 Conduix 운영툴 전체를 Kubernetes에 배포합니다.

### 포함된 구성요소

| 컴포넌트 | 설명 |
|---------|------|
| Control Plane | Go 기반 API 서버 |
| Web UI | React 기반 프론트엔드 |
| MySQL | 운영 데이터베이스 (Bitnami subchart) |
| Redis | 캐시 및 메트릭 저장소 (Bitnami subchart) |

## 사전 요구사항

- Kubernetes 1.23+
- Helm 3.8+
- PV provisioner (MySQL, Redis 영구 저장용)
- Ingress Controller (권장: nginx-ingress)

## 빠른 시작

### 1. Helm 저장소 추가 (dependencies)

```bash
helm repo add bitnami https://charts.bitnami.com/bitnami
helm repo update
```

### 2. 의존성 다운로드

```bash
cd deploy/helm/conduix
helm dependency update
```

### 3. 설치 (기본값)

```bash
helm install conduix ./conduix -n conduix --create-namespace
```

### 4. 커스텀 설정으로 설치

```bash
# values-override.yaml 작성 후
helm install conduix ./conduix \
  -n conduix \
  --create-namespace \
  -f values-override.yaml
```

## 설정 옵션

### 필수 설정 (프로덕션)

`values-production.yaml` 예시:

```yaml
# JWT Secret (필수 - 반드시 변경)
secrets:
  jwtSecret: "your-secure-32-character-secret"

# MySQL 설정
mysql:
  auth:
    rootPassword: "secure-root-password"
    password: "secure-user-password"
  primary:
    persistence:
      size: 50Gi

# Redis 설정
redis:
  auth:
    enabled: true
    password: "secure-redis-password"
  master:
    persistence:
      size: 10Gi

# Ingress 설정
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: pipeline.yourdomain.com
      paths:
        - path: /
          pathType: Prefix
          service: web-ui
        - path: /api
          pathType: Prefix
          service: control-plane
  tls:
    - secretName: pipeline-tls
      hosts:
        - pipeline.yourdomain.com

# OAuth2 설정 (GitHub)
controlPlane:
  oauth2:
    github:
      enabled: true
      clientId: "your-github-client-id"
      clientSecret: "your-github-client-secret"
      redirectUrl: "https://pipeline.yourdomain.com/api/v1/auth/callback"
```

### Docker 이미지 빌드

먼저 Docker 이미지를 빌드하고 레지스트리에 푸시해야 합니다:

```bash
# Control Plane
docker build -t your-registry/conduix/control-plane:latest \
  -f deploy/docker/Dockerfile.control-plane .
docker push your-registry/conduix/control-plane:latest

# Web UI
docker build -t your-registry/conduix/web-ui:latest \
  -f deploy/docker/Dockerfile.web-ui .
docker push your-registry/conduix/web-ui:latest
```

values.yaml에서 이미지 저장소 설정:

```yaml
controlPlane:
  image:
    repository: your-registry/conduix/control-plane
    tag: "latest"

webUI:
  image:
    repository: your-registry/conduix/web-ui
    tag: "latest"
```

### 외부 데이터베이스 사용

내장 MySQL 대신 외부 DB를 사용하려면:

```yaml
mysql:
  enabled: false

# 외부 DB 연결 설정은 ConfigMap에서
controlPlane:
  externalDatabase:
    host: "your-external-mysql.example.com"
    port: 3306
    user: "vpuser"
    password: "password"  # Secret 사용 권장
    database: "conduix"
```

## 배포 확인

```bash
# Pod 상태 확인
kubectl get pods -n conduix

# 서비스 확인
kubectl get svc -n conduix

# 로그 확인
kubectl logs -f deployment/conduix-control-plane -n conduix

# 마이그레이션 Job 확인
kubectl get jobs -n conduix
```

## 업그레이드

```bash
helm upgrade conduix ./conduix \
  -n conduix \
  -f values-production.yaml
```

## 삭제

```bash
# 애플리케이션만 삭제 (PVC 유지)
helm uninstall conduix -n conduix

# 전체 삭제 (데이터 포함)
helm uninstall conduix -n conduix
kubectl delete pvc -l app.kubernetes.io/instance=conduix -n conduix
kubectl delete namespace conduix
```

## 트러블슈팅

### MySQL 연결 실패

```bash
# MySQL Pod 상태 확인
kubectl get pods -l app.kubernetes.io/name=mysql -n conduix

# MySQL 로그 확인
kubectl logs -l app.kubernetes.io/name=mysql -n conduix
```

### 마이그레이션 Job 실패

```bash
# Job 로그 확인
kubectl logs job/conduix-migration -n conduix

# Job 재실행
kubectl delete job conduix-migration -n conduix
helm upgrade conduix ./conduix -n conduix
```

### Ingress 접근 불가

```bash
# Ingress 상태 확인
kubectl get ingress -n conduix
kubectl describe ingress conduix -n conduix
```

## 로컬 개발용 설정

Minikube 또는 Kind에서 테스트:

```yaml
# values-local.yaml
ingress:
  enabled: false

controlPlane:
  service:
    type: NodePort
    nodePort: 30080

webUI:
  service:
    type: NodePort
    nodePort: 30000

mysql:
  primary:
    persistence:
      size: 1Gi

redis:
  master:
    persistence:
      size: 1Gi
```

```bash
# 로컬 설치
helm install conduix ./conduix -f values-local.yaml

# 접근
# API: http://localhost:30080
# UI: http://localhost:30000
```

## 파일 구조

```
deploy/helm/conduix/
├── Chart.yaml              # Chart 메타데이터
├── values.yaml             # 기본 설정값
├── templates/
│   ├── _helpers.tpl        # 템플릿 헬퍼 함수
│   ├── control-plane-deployment.yaml
│   ├── web-ui-deployment.yaml
│   ├── agent-deployment.yaml
│   ├── services.yaml
│   ├── ingress.yaml
│   ├── configmaps.yaml
│   ├── secrets.yaml
│   ├── serviceaccount.yaml
│   └── migration-job.yaml  # DB 마이그레이션
└── README.md
```

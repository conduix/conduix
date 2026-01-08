# ArgoCD로 Conduix 배포하기

## 사전 요구사항

1. ArgoCD가 설치된 Kubernetes 클러스터
2. GitHub Container Registry 접근 권한 (GHCR)

## 설치 방법

### 1. GHCR Image Pull Secret 생성

```bash
# conduix 네임스페이스 생성
kubectl create namespace conduix

# GitHub Personal Access Token으로 secret 생성
kubectl create secret docker-registry ghcr-secret \
  --docker-server=ghcr.io \
  --docker-username=<github-username> \
  --docker-password=<github-token> \
  -n conduix
```

### 2. ArgoCD Application 배포

#### 옵션 A: 내장 MySQL/Redis 사용 (개발/테스트)

```bash
kubectl apply -f application.yaml
```

#### 옵션 B: 외부 MySQL/Redis 사용 (프로덕션)

```bash
# 먼저 application-external-db.yaml의 DB 설정을 수정하세요
kubectl apply -f application-external-db.yaml
```

### 3. 배포 상태 확인

```bash
# ArgoCD UI에서 확인
argocd app get conduix

# 또는 CLI로 확인
kubectl get pods -n conduix
```

## Application 파일 설명

| 파일 | 설명 |
|------|------|
| `application.yaml` | 기본 설정 (내장 MySQL/Redis) |
| `application-external-db.yaml` | 외부 DB 사용 설정 |

## 주요 설정 옵션

### 이미지 레지스트리

```yaml
parameters:
  - name: global.imagePullSecrets[0].name
    value: ghcr-secret
```

### 외부 데이터베이스

```yaml
parameters:
  - name: mysql.enabled
    value: "false"
  - name: externalDatabase.host
    value: "your-mysql-host"
  - name: externalDatabase.port
    value: "3306"
```

### 외부 Redis

```yaml
parameters:
  - name: redis.enabled
    value: "false"
  - name: externalRedis.host
    value: "your-redis-host"
```

### Ingress 설정

```yaml
parameters:
  - name: ingress.hosts[0].host
    value: "conduix.yourdomain.com"
```

### OAuth2 설정 (GitHub)

```yaml
parameters:
  - name: controlPlane.oauth2.github.enabled
    value: "true"
  - name: controlPlane.oauth2.github.clientId
    value: "your-client-id"
  - name: controlPlane.oauth2.github.clientSecret
    value: "your-client-secret"
```

## 트러블슈팅

### Image Pull 실패

```bash
# Secret 확인
kubectl get secret ghcr-secret -n conduix -o yaml

# Pod 이벤트 확인
kubectl describe pod <pod-name> -n conduix
```

### DB 연결 실패

```bash
# Control Plane 로그 확인
kubectl logs -l app.kubernetes.io/component=control-plane -n conduix
```

### Sync 실패

```bash
# ArgoCD 앱 상태 확인
argocd app get conduix --show-params
argocd app sync conduix --dry-run
```

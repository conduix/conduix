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
# 먼저 application-external-db.yaml의 설정을 수정하세요
kubectl apply -f application-external-db.yaml
```

### 3. 배포 상태 확인

```bash
# ArgoCD UI에서 확인
argocd app get conduix

# 또는 CLI로 확인
kubectl get pods -n conduix
```

## 환경변수 설정 구조

### ConfigMap (env.*)

일반 환경변수는 `env.*` 파라미터로 설정하며, ConfigMap으로 주입됩니다.

```yaml
parameters:
  # Database
  - name: env.DB_HOST
    value: "your-mysql-host"
  - name: env.DB_PORT
    value: "3306"
  - name: env.DB_USER
    value: "conduixuser"
  - name: env.DB_NAME
    value: "conduix"

  # Redis
  - name: env.REDIS_ADDR
    value: "your-redis-host:6379"

  # Application
  - name: env.AUTO_MIGRATE
    value: "true"
  - name: env.FRONTEND_URL
    value: "https://conduix.yourdomain.com"

  # OAuth2 Settings
  - name: env.OAUTH_REDIRECT_URL
    value: "https://conduix.yourdomain.com/api/v1/auth/callback"
  - name: env.GITHUB_CLIENT_ID
    value: "your-client-id"
```

### Secret (secrets.*)

민감한 정보는 `secrets.*` 파라미터로 설정하며, Secret으로 주입됩니다.

```yaml
parameters:
  # JWT Secret
  - name: secrets.jwtSecret
    value: "your-secure-jwt-secret"

  # Database Password
  - name: secrets.dbPassword
    value: "your-db-password"

  # Redis Password
  - name: secrets.redisPassword
    value: ""

  # OAuth2 Client Secrets
  - name: secrets.oauthGithubClientSecret
    value: "your-github-client-secret"
  - name: secrets.oauthGoogleClientSecret
    value: "your-google-client-secret"
```

## 전체 환경변수 목록

### env.* (ConfigMap)

| 파라미터 | 설명 | 기본값 |
|---------|------|--------|
| `env.DB_HOST` | MySQL 호스트 | `host.docker.internal` |
| `env.DB_PORT` | MySQL 포트 | `3307` |
| `env.DB_USER` | MySQL 사용자 | `conduixuser` |
| `env.DB_NAME` | 데이터베이스 이름 | `conduix` |
| `env.REDIS_ADDR` | Redis 주소 | `host.docker.internal:6379` |
| `env.AUTO_MIGRATE` | 자동 마이그레이션 | `true` |
| `env.MAX_DATATYPE_DEPTH` | DataType 최대 깊이 | `10` |
| `env.FRONTEND_URL` | 프론트엔드 URL | `http://localhost:3000` |
| `env.CONDUIX_ADMIN_EMAILS` | Admin 사용자 이메일 (콤마 구분) | - |
| `env.CONDUIX_OPERATOR_EMAILS` | Operator 사용자 이메일 (콤마 구분) | - |
| `env.OAUTH_REDIRECT_URL` | OAuth 공통 Redirect URL | - |
| `env.GITHUB_CLIENT_ID` | GitHub OAuth Client ID | - |
| `env.GOOGLE_CLIENT_ID` | Google OAuth Client ID | - |
| `env.NAVER_CLIENT_ID` | Naver OAuth Client ID | - |
| `env.KAKAO_CLIENT_ID` | Kakao OAuth Client ID | - |

### secrets.* (Secret)

| 파라미터 | 설명 | 기본값 |
|---------|------|--------|
| `secrets.jwtSecret` | JWT 시크릿 키 | 자동 생성 |
| `secrets.dbPassword` | DB 비밀번호 | `conduixpassword` |
| `secrets.redisPassword` | Redis 비밀번호 | - |
| `secrets.oauthGithubClientSecret` | GitHub OAuth Secret | - |
| `secrets.oauthGoogleClientSecret` | Google OAuth Secret | - |
| `secrets.oauthNaverClientSecret` | Naver OAuth Secret | - |
| `secrets.oauthKakaoClientSecret` | Kakao OAuth Secret | - |

## 프로덕션 보안 권장사항

### External Secrets Operator 사용

프로덕션 환경에서는 민감한 정보를 Git에 저장하지 않고, External Secrets Operator를 사용하여 AWS Secrets Manager, HashiCorp Vault 등에서 가져오는 것을 권장합니다.

```yaml
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata:
  name: conduix-secrets
  namespace: conduix
spec:
  refreshInterval: 1h
  secretStoreRef:
    kind: ClusterSecretStore
    name: aws-secrets-manager
  target:
    name: conduix-control-plane-secrets
  data:
    - secretKey: JWT_SECRET
      remoteRef:
        key: conduix/production
        property: jwt_secret
    - secretKey: DB_PASSWORD
      remoteRef:
        key: conduix/production
        property: db_password
```

## 트러블슈팅

### ConfigMap/Secret 변경이 반영되지 않을 때

Pod annotation에 checksum이 포함되어 있어 ConfigMap/Secret 변경 시 자동으로 재시작됩니다. 수동 재시작이 필요한 경우:

```bash
kubectl rollout restart deployment conduix-control-plane -n conduix
kubectl rollout restart deployment conduix-agent -n conduix
```

### Image Pull 실패

```bash
# Secret 확인
kubectl get secret ghcr-secret -n conduix -o yaml

# Pod 이벤트 확인
kubectl describe pod <pod-name> -n conduix
```

### DB 연결 실패

```bash
# ConfigMap 확인
kubectl get configmap conduix-control-plane-env -n conduix -o yaml

# Secret 확인
kubectl get secret conduix-control-plane-secrets -n conduix -o yaml

# Control Plane 로그 확인
kubectl logs -l app.kubernetes.io/component=control-plane -n conduix
```

### Sync 실패

```bash
# ArgoCD 앱 상태 확인
argocd app get conduix --show-params
argocd app sync conduix --dry-run
```

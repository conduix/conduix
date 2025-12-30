# Deploy

배포 구성 및 스크립트

## 개요

`deploy`는 Conduix 시스템을 다양한 환경에 배포하기 위한 설정과 스크립트를 포함합니다.

## 디렉토리 구조

```
deploy/
├── docker/
│   ├── Dockerfile.control-plane   # Control Plane Docker 이미지
│   ├── Dockerfile.agent           # Agent Docker 이미지
│   ├── Dockerfile.web-ui          # Web UI Docker 이미지
│   ├── nginx.conf                 # Web UI Nginx 설정
│   └── mysql/
│       └── init.sql               # MySQL 초기화 스크립트
├── helm/
│   └── conduix/           # Kubernetes Helm 차트
│       ├── Chart.yaml
│       ├── values.yaml
│       └── templates/
│           ├── _helpers.tpl
│           ├── control-plane-deployment.yaml
│           ├── agent-deployment.yaml
│           ├── web-ui-deployment.yaml
│           ├── services.yaml
│           ├── configmaps.yaml
│           ├── secrets.yaml
│           ├── ingress.yaml
│           └── serviceaccount.yaml
└── scripts/
    └── install.sh                 # 물리서버 설치 스크립트
```

## 배포 방식

### 1. Docker Compose (개발/테스트)

로컬 개발 및 테스트 환경에 적합합니다.

```bash
# 프로젝트 루트에서 실행
cd conduix
docker-compose up -d
```

#### docker-compose.yml 구성

```yaml
services:
  mysql:
    image: mysql:8.0
    environment:
      MYSQL_ROOT_PASSWORD: rootpassword
      MYSQL_DATABASE: conduix
      MYSQL_USER: vpuser
      MYSQL_PASSWORD: vppassword
    volumes:
      - mysql_data:/var/lib/mysql
      - ./deploy/docker/mysql/init.sql:/docker-entrypoint-initdb.d/init.sql
    ports:
      - "3306:3306"

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  control-plane:
    build:
      context: .
      dockerfile: deploy/docker/Dockerfile.control-plane
    environment:
      - DB_HOST=mysql
      - REDIS_HOST=redis
      - JWT_SECRET=change-me
    ports:
      - "8080:8080"
    depends_on:
      - mysql
      - redis

  agent:
    build:
      context: .
      dockerfile: deploy/docker/Dockerfile.agent
    environment:
      - CONTROL_PLANE_URL=http://control-plane:8080
      - REDIS_HOST=redis
    depends_on:
      - control-plane
      - redis

  web-ui:
    build:
      context: .
      dockerfile: deploy/docker/Dockerfile.web-ui
    ports:
      - "3000:80"
    depends_on:
      - control-plane
```

### 2. Kubernetes + Helm (프로덕션)

프로덕션 환경에 권장되는 배포 방식입니다.

#### 기본 설치

```bash
cd deploy/helm

# 의존성 설치 (MySQL, Redis)
helm repo add bitnami https://charts.bitnami.com/bitnami
helm dependency update conduix

# 설치
helm install vp conduix \
  --namespace conduix \
  --create-namespace
```

#### 커스텀 설정

```bash
# values 파일 생성
cat > my-values.yaml << EOF
controlPlane:
  replicaCount: 3
  env:
    JWT_SECRET: "my-production-secret"

agent:
  replicaCount: 5
  autoscaling:
    enabled: true
    minReplicas: 3
    maxReplicas: 20

mysql:
  auth:
    rootPassword: "secure-root-password"
    password: "secure-user-password"

ingress:
  enabled: true
  hosts:
    - host: pipeline.example.com
      paths:
        - path: /
  tls:
    - secretName: pipeline-tls
      hosts:
        - pipeline.example.com
EOF

# 커스텀 설정으로 설치
helm install vp conduix -f my-values.yaml
```

#### 주요 values.yaml 설정

```yaml
# Global
global:
  imagePullSecrets: []

# Control Plane
controlPlane:
  replicaCount: 2
  image:
    repository: conduix/control-plane
    tag: latest
  resources:
    limits:
      cpu: 500m
      memory: 512Mi
  env:
    JWT_SECRET: "change-me-in-production"

# Agent
agent:
  replicaCount: 3
  image:
    repository: conduix/agent
    tag: latest
  resources:
    limits:
      cpu: 1000m
      memory: 1Gi
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 20
    targetCPUUtilizationPercentage: 70

# Web UI
webUI:
  replicaCount: 2
  image:
    repository: conduix/web-ui
    tag: latest

# Ingress
ingress:
  enabled: true
  className: nginx
  hosts:
    - host: conduix.local
  tls: []

# MySQL (Bitnami subchart)
mysql:
  enabled: true
  auth:
    database: conduix
    username: vpuser
    password: vppassword

# Redis (Bitnami subchart)
redis:
  enabled: true
  architecture: standalone
  auth:
    enabled: false
```

### 3. 물리서버 설치

베어메탈 또는 VM에 직접 설치합니다.

#### 사전 요구사항

- Go 1.21+
- Node.js 18+
- MySQL 8.0+
- Redis 7.0+
- systemd

#### 설치

```bash
# 설치 스크립트 실행
sudo ./deploy/scripts/install.sh
```

#### 수동 설치

```bash
# 1. 시스템 사용자 생성
sudo useradd --system --no-create-home conduix

# 2. 디렉토리 생성
sudo mkdir -p /opt/conduix
sudo mkdir -p /etc/conduix
sudo mkdir -p /var/log/conduix
sudo mkdir -p /var/lib/conduix

# 3. 바이너리 복사
sudo cp control-plane /opt/conduix/
sudo cp agent /opt/conduix/

# 4. 설정 파일 복사
sudo cp config.yaml /etc/conduix/

# 5. 권한 설정
sudo chown -R conduix:conduix /opt/conduix
sudo chown -R conduix:conduix /var/log/conduix
sudo chown -R conduix:conduix /var/lib/conduix

# 6. systemd 서비스 등록
sudo cp conduix-*.service /etc/systemd/system/
sudo systemctl daemon-reload

# 7. 서비스 시작
sudo systemctl enable conduix-control-plane
sudo systemctl start conduix-control-plane
sudo systemctl enable conduix-agent
sudo systemctl start conduix-agent
```

#### systemd 서비스 파일

```ini
# /etc/systemd/system/conduix-control-plane.service
[Unit]
Description=Conduix Control Plane
After=network.target mysql.service redis.service

[Service]
Type=simple
User=conduix
Group=conduix
EnvironmentFile=/etc/conduix/.env
ExecStart=/opt/conduix/control-plane --migrate
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```ini
# /etc/systemd/system/conduix-agent.service
[Unit]
Description=Conduix Agent
After=network.target conduix-control-plane.service

[Service]
Type=simple
User=conduix
Group=conduix
EnvironmentFile=/etc/conduix/.env
ExecStart=/opt/conduix/agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

## Docker 이미지

### Control Plane

```dockerfile
# deploy/docker/Dockerfile.control-plane
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN cd control-plane && go build -o /control-plane ./cmd/server

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /control-plane /usr/local/bin/
EXPOSE 8080
ENTRYPOINT ["control-plane"]
CMD ["--migrate"]
```

### Agent

```dockerfile
# deploy/docker/Dockerfile.agent
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN cd pipeline-agent && go build -o /agent ./cmd/agent

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /agent /usr/local/bin/
EXPOSE 8081
ENTRYPOINT ["agent"]
```

### Web UI

```dockerfile
# deploy/docker/Dockerfile.web-ui
FROM node:18-alpine AS builder
WORKDIR /app
COPY web-ui/package*.json ./
RUN npm ci
COPY web-ui/ .
RUN npm run build

FROM nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY deploy/docker/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
```

### 이미지 빌드

```bash
# 개별 빌드
docker build -t conduix/control-plane:latest \
  -f deploy/docker/Dockerfile.control-plane .

docker build -t conduix/agent:latest \
  -f deploy/docker/Dockerfile.agent .

docker build -t conduix/web-ui:latest \
  -f deploy/docker/Dockerfile.web-ui .

# 전체 빌드 (Makefile)
make docker-build
```

## Helm 차트 상세

### 차트 구조

```
helm/conduix/
├── Chart.yaml          # 차트 메타데이터
├── values.yaml         # 기본 설정값
├── templates/
│   ├── _helpers.tpl                    # 템플릿 헬퍼 함수
│   ├── control-plane-deployment.yaml   # Control Plane Deployment
│   ├── agent-deployment.yaml           # Agent Deployment
│   ├── web-ui-deployment.yaml          # Web UI Deployment
│   ├── services.yaml                   # Services (ClusterIP)
│   ├── configmaps.yaml                 # ConfigMaps
│   ├── secrets.yaml                    # Secrets
│   ├── ingress.yaml                    # Ingress
│   └── serviceaccount.yaml             # ServiceAccount
```

### 의존성

```yaml
# Chart.yaml
dependencies:
  - name: mysql
    version: "9.x.x"
    repository: https://charts.bitnami.com/bitnami
    condition: mysql.enabled
  - name: redis
    version: "18.x.x"
    repository: https://charts.bitnami.com/bitnami
    condition: redis.enabled
```

### Helm 명령어

```bash
# 설치
helm install vp ./deploy/helm/conduix

# 업그레이드
helm upgrade vp ./deploy/helm/conduix

# 삭제
helm uninstall vp

# 값 확인
helm get values vp

# 매니페스트 확인 (dry-run)
helm template vp ./deploy/helm/conduix

# 차트 검증
helm lint ./deploy/helm/conduix
```

## 환경별 설정

### 개발 환경

```yaml
# values-dev.yaml
controlPlane:
  replicaCount: 1
  resources:
    limits:
      cpu: 200m
      memory: 256Mi

agent:
  replicaCount: 1
  autoscaling:
    enabled: false

webUI:
  replicaCount: 1

ingress:
  enabled: false
```

### 스테이징 환경

```yaml
# values-staging.yaml
controlPlane:
  replicaCount: 2

agent:
  replicaCount: 2
  autoscaling:
    enabled: true
    minReplicas: 2
    maxReplicas: 5

ingress:
  enabled: true
  hosts:
    - host: pipeline-staging.example.com
```

### 프로덕션 환경

```yaml
# values-prod.yaml
controlPlane:
  replicaCount: 3
  resources:
    limits:
      cpu: 1000m
      memory: 1Gi

agent:
  replicaCount: 5
  autoscaling:
    enabled: true
    minReplicas: 5
    maxReplicas: 50
  resources:
    limits:
      cpu: 2000m
      memory: 4Gi

mysql:
  primary:
    persistence:
      size: 100Gi
  secondary:
    replicaCount: 2

redis:
  architecture: replication
  replica:
    replicaCount: 2

ingress:
  enabled: true
  hosts:
    - host: pipeline.example.com
  tls:
    - secretName: pipeline-tls
      hosts:
        - pipeline.example.com
```

## 모니터링 설정

### Prometheus ServiceMonitor

```yaml
# templates/servicemonitor.yaml
{{- if .Values.serviceMonitor.enabled }}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: {{ include "conduix.fullname" . }}
spec:
  selector:
    matchLabels:
      {{- include "conduix.selectorLabels" . | nindent 6 }}
  endpoints:
    - port: metrics
      interval: 30s
{{- end }}
```

### Grafana 대시보드

```bash
# ConfigMap으로 대시보드 배포
kubectl create configmap grafana-dashboard \
  --from-file=dashboard.json \
  -n monitoring
```

## 업그레이드 가이드

### 1. 버전 확인

```bash
helm list -n conduix
```

### 2. 백업

```bash
# MySQL 백업
kubectl exec -it mysql-0 -- mysqldump -u root -p conduix > backup.sql

# Redis 백업 (RDB)
kubectl exec -it redis-master-0 -- redis-cli BGSAVE
```

### 3. 업그레이드

```bash
# 이미지 업데이트
helm upgrade vp ./deploy/helm/conduix \
  --set controlPlane.image.tag=v1.2.0 \
  --set agent.image.tag=v1.2.0 \
  --set webUI.image.tag=v1.2.0
```

### 4. 롤백 (필요시)

```bash
# 이전 릴리스로 롤백
helm rollback vp 1
```

## 트러블슈팅

### Pod 상태 확인

```bash
kubectl get pods -n conduix
kubectl describe pod <pod-name> -n conduix
kubectl logs <pod-name> -n conduix
```

### 일반적인 문제

#### MySQL 연결 실패

```bash
# MySQL Pod 상태 확인
kubectl get pods -l app.kubernetes.io/name=mysql

# 연결 테스트
kubectl exec -it control-plane-xxx -- nc -zv mysql 3306
```

#### Redis 연결 실패

```bash
# Redis Pod 상태 확인
kubectl get pods -l app.kubernetes.io/name=redis

# 연결 테스트
kubectl exec -it control-plane-xxx -- redis-cli -h redis-master ping
```

#### Ingress 접속 불가

```bash
# Ingress 상태 확인
kubectl get ingress -n conduix
kubectl describe ingress -n conduix

# Ingress Controller 로그
kubectl logs -n ingress-nginx -l app.kubernetes.io/name=ingress-nginx
```

## 관련 문서

- [Control Plane](../control-plane/README.md)
- [Pipeline Agent](../pipeline-agent/README.md)
- [Web UI](../web-ui/README.md)

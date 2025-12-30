#!/bin/bash
set -e

# 색상 정의
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Conduix 설치 스크립트 ===${NC}"

# 변수
INSTALL_DIR=${INSTALL_DIR:-/opt/conduix}
DATA_DIR=${DATA_DIR:-/var/lib/conduix}
CONFIG_DIR=${CONFIG_DIR:-/etc/conduix}
LOG_DIR=${LOG_DIR:-/var/log/conduix}
USER=${VP_USER:-conduix}
GROUP=${VP_GROUP:-conduix}

# 필수 도구 확인
check_requirements() {
    echo -e "${YELLOW}필수 도구 확인 중...${NC}"

    for cmd in docker docker-compose mysql redis-cli; do
        if ! command -v $cmd &> /dev/null; then
            echo -e "${RED}오류: $cmd 가 설치되어 있지 않습니다.${NC}"
            exit 1
        fi
    done

    echo -e "${GREEN}모든 필수 도구가 설치되어 있습니다.${NC}"
}

# 사용자 및 디렉토리 생성
setup_directories() {
    echo -e "${YELLOW}디렉토리 설정 중...${NC}"

    # 사용자/그룹 생성
    if ! id "$USER" &>/dev/null; then
        sudo useradd --system --no-create-home --shell /bin/false "$USER"
    fi

    # 디렉토리 생성
    sudo mkdir -p "$INSTALL_DIR"
    sudo mkdir -p "$DATA_DIR"/{mysql,redis}
    sudo mkdir -p "$CONFIG_DIR"
    sudo mkdir -p "$LOG_DIR"

    sudo chown -R "$USER:$GROUP" "$DATA_DIR"
    sudo chown -R "$USER:$GROUP" "$LOG_DIR"

    echo -e "${GREEN}디렉토리 설정 완료${NC}"
}

# 설정 파일 복사
copy_configs() {
    echo -e "${YELLOW}설정 파일 복사 중...${NC}"

    # docker-compose.yml 복사
    sudo cp docker-compose.yml "$CONFIG_DIR/"

    # 환경 변수 파일 생성
    if [ ! -f "$CONFIG_DIR/.env" ]; then
        cat > /tmp/vp.env << EOF
# Conduix 환경 변수
DB_HOST=localhost
DB_PORT=3306
DB_USER=vpuser
DB_PASSWORD=vppassword
DB_NAME=conduix

REDIS_HOST=localhost
REDIS_PORT=6379

JWT_SECRET=$(openssl rand -base64 32)

CONTROL_PLANE_PORT=8080
AGENT_PORT=8081
WEB_UI_PORT=3000
EOF
        sudo mv /tmp/vp.env "$CONFIG_DIR/.env"
    fi

    sudo chown root:root "$CONFIG_DIR/.env"
    sudo chmod 600 "$CONFIG_DIR/.env"

    echo -e "${GREEN}설정 파일 복사 완료${NC}"
}

# systemd 서비스 생성
create_services() {
    echo -e "${YELLOW}systemd 서비스 생성 중...${NC}"

    # Control Plane 서비스
    cat > /tmp/conduix-control-plane.service << EOF
[Unit]
Description=Conduix Control Plane
After=network.target mysql.service redis.service

[Service]
Type=simple
User=$USER
Group=$GROUP
EnvironmentFile=$CONFIG_DIR/.env
ExecStart=$INSTALL_DIR/control-plane --migrate
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

    # Agent 서비스
    cat > /tmp/conduix-agent.service << EOF
[Unit]
Description=Conduix Agent
After=network.target conduix-control-plane.service

[Service]
Type=simple
User=$USER
Group=$GROUP
EnvironmentFile=$CONFIG_DIR/.env
ExecStart=$INSTALL_DIR/agent
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

    sudo mv /tmp/conduix-control-plane.service /etc/systemd/system/
    sudo mv /tmp/conduix-agent.service /etc/systemd/system/

    sudo systemctl daemon-reload

    echo -e "${GREEN}systemd 서비스 생성 완료${NC}"
}

# 서비스 시작
start_services() {
    echo -e "${YELLOW}서비스 시작 중...${NC}"

    sudo systemctl enable conduix-control-plane
    sudo systemctl enable conduix-agent
    sudo systemctl start conduix-control-plane
    sudo systemctl start conduix-agent

    echo -e "${GREEN}서비스 시작 완료${NC}"
}

# 상태 확인
check_status() {
    echo -e "${YELLOW}서비스 상태 확인 중...${NC}"

    echo ""
    systemctl status conduix-control-plane --no-pager || true
    echo ""
    systemctl status conduix-agent --no-pager || true
}

# 메인
main() {
    check_requirements
    setup_directories
    copy_configs
    create_services

    echo ""
    echo -e "${GREEN}=== 설치 완료 ===${NC}"
    echo ""
    echo "서비스를 시작하려면:"
    echo "  sudo systemctl start conduix-control-plane"
    echo "  sudo systemctl start conduix-agent"
    echo ""
    echo "웹 UI 접속: http://localhost:3000"
    echo "API 접속: http://localhost:8080"
}

main "$@"

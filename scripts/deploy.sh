#!/usr/bin/env bash

set -euo pipefail

APP_NAME="WaveYo-Koishi-Mirror"
INSTALL_DIR="/opt/waveyo-koishi-mirror"
BIN_NAME="koishi-mirror"
SERVICE_NAME="waveyo-koishi-mirror.service"

# 版本配置
GO_VERSION="1.23.4"  # 最新 LTS 版本
NODE_VERSION="22.14.0"  # Node.js 22 LTS
NPM_VERSION="latest"

usage() {
  cat <<EOF
用法: $0 [--env /path/to/.env] [--set KEY=VALUE ...] [--service-name NAME] [--install-deps]

选项:
  --env PATH           指定要部署的 .env 文件路径
  --set KEY=VALUE      在部署前修改/覆盖 .env 中的键值，多次使用支持多个键
  --service-name NAME  自定义 systemd 服务名 (默认: ${SERVICE_NAME})
  --install-deps       自动安装依赖 (Go, Node.js, npm)
  --go-version VER     指定 Go 版本 (默认: ${GO_VERSION})
  --node-version VER   指定 Node.js 版本 (默认: ${NODE_VERSION})
  --skip-build         跳过构建步骤，仅部署
  --skip-frontend      跳过前端构建
  --skip-backend       跳过后端构建
  --non-interactive    非交互模式，跳过编辑配置
  --no-restart         不重启服务（即使检测到变更）
  --force-restart      强制重启服务（无论是否检测到变更）

示例:
  sudo bash scripts/deploy.sh --env ./prod.env --set API_PORT=8080 --set LOG_LEVEL=info --install-deps
EOF
}

ENV_SRC=""
DECLARED_KV=()
INSTALL_DEPS=false
SKIP_BUILD=false
SKIP_FRONTEND=false
SKIP_BACKEND=false
NON_INTERACTIVE=false
NO_RESTART=false
FORCE_RESTART=false
PACKAGE_MANAGER=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env)
      ENV_SRC="$2"; shift 2;;
    --set)
      DECLARED_KV+=("$2"); shift 2;;
    --service-name)
      SERVICE_NAME="$2"; shift 2;;
    --install-deps)
      INSTALL_DEPS=true; shift;;
    --go-version)
      GO_VERSION="$2"; shift 2;;
    --node-version)
      NODE_VERSION="$2"; shift 2;;
    --skip-build)
      SKIP_BUILD=true; shift;;
    --skip-frontend)
      SKIP_FRONTEND=true; shift;;
    --skip-backend)
      SKIP_BACKEND=true; shift;;
    --non-interactive)
      NON_INTERACTIVE=true; shift;;
    --no-restart)
      NO_RESTART=true; shift;;
    --force-restart)
      FORCE_RESTART=true; shift;;
    -h|--help)
      usage; exit 0;;
    *)
      echo "未知参数: $1"; usage; exit 1;;
  esac
done

# 颜色输出
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

# 检查是否为 root 用户
check_root() {
  if [[ $EUID -ne 0 ]]; then
    log_error "此脚本需要 root 权限运行"
    log_info "请使用: sudo $0 $*"
    exit 1
  fi
}

# 检查系统依赖
check_system_deps() {
  log_info "检查系统依赖..."

  local missing_deps=()

  # 检查 bc（用于版本比较）
  if ! command -v bc &> /dev/null; then
    missing_deps+=("bc")
  fi

  # 检查 rsync
  if ! command -v rsync &> /dev/null; then
    missing_deps+=("rsync")
  fi

  # 检查其他必要工具
  if ! command -v curl &> /dev/null; then
    missing_deps+=("curl")
  fi

  if ! command -v wget &> /dev/null; then
    missing_deps+=("wget")
  fi

  if [[ ${#missing_deps[@]} -gt 0 ]]; then
    log_warning "缺少系统依赖: ${missing_deps[*]}"
  
    if [[ "$INSTALL_DEPS" == true ]]; then
      log_info "安装系统依赖..."
      apt-get update
      apt-get install -y "${missing_deps[@]}"
      log_success "系统依赖安装完成"
    else
      log_warning "请手动安装: sudo apt-get install ${missing_deps[*]}"
      log_info "或使用 --install-deps 参数自动安装"
    fi
  else
    log_success "系统依赖检查通过"
  fi
}

# 版本比较函数（不使用 bc）
compare_versions() {
  local ver1=$1
  local ver2=$2

  # 使用 awk 进行版本比较
  echo "$ver1 $ver2" | awk '{
    split($1, a, ".")
    split($2, b, ".")
  
    for (i=1; i<=3; i++) {
      a[i] = a[i] + 0  # 转换为数字
      b[i] = b[i] + 0
    
      if (a[i] < b[i]) {
        print -1
        exit
      }
      if (a[i] > b[i]) {
        print 1
        exit
      }
    }
    print 0
  }'
}

# 安装依赖
install_dependencies() {
  log_info "安装系统依赖..."
  
  # 更新包列表
  apt-get update
  
  # 安装基础工具（包括 bc 和 rsync）
  apt-get install -y curl wget git build-essential ca-certificates gnupg lsb-release bc rsync
  
  # 安装 Go
  if ! command -v go &> /dev/null || [[ "$(compare_versions "$(go version | grep -oP 'go\K[0-9]+\.[0-9]+(\.[0-9]+)?')" "1.23")" -lt 0 ]]; then
    log_info "安装 Go ${GO_VERSION}..."
    GO_TAR="go${GO_VERSION}.linux-amd64.tar.gz"
    wget -q "https://golang.org/dl/${GO_TAR}" -O /tmp/${GO_TAR}
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/${GO_TAR}
    rm /tmp/${GO_TAR}
    
    # 添加到 PATH
    if ! grep -q "/usr/local/go/bin" /etc/profile; then
      echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
    fi
    export PATH=$PATH:/usr/local/go/bin
  fi
  
  # 安装 Node.js (使用 NodeSource)
  if ! command -v node &> /dev/null || [[ "$(node -v | cut -d'v' -f2 | cut -d'.' -f1)" -lt 22 ]]; then
    log_info "安装 Node.js ${NODE_VERSION}..."
    
    # 清理旧版本
    apt-get remove -y nodejs npm
    
    # 安装 NodeSource 仓库
    NODE_MAJOR=$(echo $NODE_VERSION | cut -d'.' -f1)
    curl -fsSL https://deb.nodesource.com/setup_${NODE_MAJOR}.x | bash -
    apt-get install -y nodejs
    
    # 安装最新 npm
    npm install -g npm@${NPM_VERSION}
  fi
  
  # 安装 corepack 并启用 pnpm
  if command -v corepack &> /dev/null; then
    corepack enable
    # 安装 pnpm
    corepack prepare pnpm@latest --activate
    log_info "已启用 pnpm"
  else
    # 如果没有 corepack，直接安装 pnpm
    npm install -g pnpm
  fi
  
  log_success "依赖安装完成"
}

# 检查并安装依赖
check_dependencies() {
  log_info "检查依赖..."
  
  # 检查系统依赖
  check_system_deps

  # 检查 Go
  if ! command -v go &> /dev/null; then
    log_warning "Go 未安装"
    if [[ "$INSTALL_DEPS" == true ]]; then
      install_dependencies
    else
      log_error "请安装 Go 或使用 --install-deps 参数"
      exit 1
    fi
  else
    GO_VER=$(go version | grep -oP 'go\K[0-9]+\.[0-9]+(\.[0-9]+)?')
    log_info "Go 版本: $GO_VER"
  
    # 使用新的版本比较函数
    GO_MIN="1.23"
    comparison=$(compare_versions "$GO_VER" "$GO_MIN")
    if [[ $comparison -lt 0 ]]; then
      log_warning "Go 版本低于 1.23，建议升级"
      if [[ "$INSTALL_DEPS" == true ]]; then
        install_dependencies
      fi
    fi
  fi
  
  # 检查 Node.js
  if ! command -v node &> /dev/null; then
    log_warning "Node.js 未安装"
    if [[ "$INSTALL_DEPS" == true ]]; then
      install_dependencies
    else
      log_error "请安装 Node.js 或使用 --install-deps 参数"
      exit 1
    fi
  else
    NODE_VER=$(node -v | cut -d'v' -f2)
    log_info "Node.js 版本: $NODE_VER"
  
    # 检查 Node.js 版本
    NODE_MAJOR=$(echo $NODE_VER | cut -d'.' -f1)
    if [[ $NODE_MAJOR -lt 22 ]]; then
      log_warning "Node.js 版本低于 22 LTS，建议升级"
      if [[ "$INSTALL_DEPS" == true ]]; then
        install_dependencies
      fi
    fi
  fi
  
  # 检查包管理器（支持 npm/yarn/pnpm）
  detect_package_manager() {
    # 优先使用 corepack 管理的包管理器
    if command -v corepack &> /dev/null; then
      log_info "检测到 corepack，启用包管理器..."
      corepack enable --all >/dev/null 2>&1 || true
    fi
  
    # 检查 pnpm
    if command -v pnpm &> /dev/null; then
      PACKAGE_MANAGER="pnpm"
      log_info "使用 pnpm 作为包管理器"
    # 默认使用 npm
    elif command -v npm &> /dev/null; then
      PACKAGE_MANAGER="npm"
      log_info "使用 npm 作为包管理器"
    else
      log_error "未找到包管理器 (npm/pnpm)"
      if [[ "$INSTALL_DEPS" == true ]]; then
        install_dependencies
      else
        exit 1
      fi
    fi
  }

  detect_package_manager
  
  # 检查 systemd
  if ! command -v systemctl &> /dev/null; then
    log_error "systemctl 未找到，请确保系统使用 systemd"
    exit 1
  fi

  log_success "依赖检查完成"
}

# 设置 PATH
setup_path() {
  export PATH="${PATH:+$PATH:}/usr/local/go/bin:/usr/local/bin:/usr/bin:/snap/bin:/bin:/usr/sbin:/sbin"
  log_info "当前 PATH: $PATH"
}

# 获取二进制路径
get_bin_paths() {
  # Go
  GO_BIN="${GO_BIN:-$(command -v go || true)}"
  if [[ -z "$GO_BIN" ]]; then
    for p in /usr/local/go/bin/go /usr/bin/go /bin/go; do
      [[ -x "$p" ]] && GO_BIN="$p" && break
    done
  fi
  
  # Node.js
  NODE_BIN="${NODE_BIN:-$(command -v node || true)}"
  if [[ -z "$NODE_BIN" ]]; then
    for p in /usr/local/bin/node /usr/bin/node /bin/node; do
      [[ -x "$p" ]] && NODE_BIN="$p" && break
    done
  fi
  
  # 包管理器路径
  case "$PACKAGE_MANAGER" in
    pnpm)
      PM_BIN="${PM_BIN:-$(command -v pnpm || true)}"
      ;;
    *)
      PM_BIN="${PM_BIN:-$(command -v npm || true)}"
      ;;
  esac
  
  # 最终检查
  if [[ -z "$GO_BIN" ]]; then
    log_error "未找到 Go 可执行文件"
    exit 1
  fi
  
  if [[ -z "$PM_BIN" ]]; then
    log_error "未找到包管理器可执行文件"
    exit 1
  fi
  
  log_info "使用: GO_BIN=$GO_BIN, NODE_BIN=$NODE_BIN, PM_BIN=$PM_BIN"
}

# 创建用户和目录
setup_user_and_dirs() {
  log_info "创建系统用户和目录..."
  
  # 创建用户
  if ! id -u waveyo >/dev/null 2>&1; then
    useradd --system --create-home --home-dir /home/waveyo --shell /usr/sbin/nologin waveyo || true
    log_success "创建用户 waveyo"
  else
    log_info "用户 waveyo 已存在"
  fi
  
  # 创建目录
  mkdir -p "${INSTALL_DIR}/bin" "${INSTALL_DIR}/ui/dist" "${INSTALL_DIR}/logs" "${INSTALL_DIR}/data"
  chown -R waveyo:waveyo "${INSTALL_DIR}"
  chmod 755 "${INSTALL_DIR}"
  
  log_success "目录结构创建完成"
}

# 设置 pnpm 配置文件
setup_pnpm_config() {
  log_info "配置 pnpm 优化..."

  local npmrc_file="ui/.npmrc"

  if [[ "$PACKAGE_MANAGER" == "pnpm" ]]; then
    cat > "$npmrc_file" << 'EOF'
# pnpm 配置
shamefully-hoist=true
strict-peer-dependencies=false
prefer-frozen-lockfile=true
auto-install-peers=true

# 清理缓存
pnpm cache clean

# 缓存配置
store-dir=.pnpm-store

# 网络优化
fetch-retries=3
fetch-timeout=60000

# 构建优化
dedupe-peer-dependents=true
resolution-mode=highest
EOF
  
    log_success "pnpm 配置已创建"
  fi
}

# 构建前端
build_frontend() {
  if [[ "$SKIP_FRONTEND" == true ]]; then
    log_info "跳过前端构建"
    return 0
  fi
  
  log_info "构建前端..."
  
  pushd ui >/dev/null
  
  # 检查 package.json
  if [[ ! -f "package.json" ]]; then
    log_error "ui/package.json 未找到"
    popd >/dev/null
    return 1
  fi

  # 设置 pnpm 配置
  setup_pnpm_config
  
  # 根据包管理器选择命令
  case "$PACKAGE_MANAGER" in
    pnpm)
      log_info "使用 pnpm 加速构建..."
      # 检查 pnpm-lock.yaml
      if [[ -f "pnpm-lock.yaml" ]]; then
        pnpm install --frozen-lockfile --silent
      else
        pnpm install --silent
      fi
      pnpm run build --silent
      ;;
    npm)
      log_info "使用 npm 构建..."
      if [[ -f "package-lock.json" ]]; then
        npm ci --silent
      else
        npm install --silent
      fi
      # 先清理缓存
      rm -rf node_modules && npm run build --silent
      ;;
  esac
  
  if [[ $? -eq 0 ]]; then
    log_success "前端构建完成"
  else
    log_error "前端构建失败"
    popd >/dev/null
    return 1
  fi
  
  popd >/dev/null
}

# 构建后端
build_backend() {
  if [[ "$SKIP_BACKEND" == true ]]; then
    log_info "跳过后端构建"
    return 0
  fi
  
  log_info "构建后端..."
  
  # 检查 go.mod
  if [[ ! -f "go.mod" ]]; then
    log_error "go.mod 未找到"
    return 1
  fi
  
  # 下载依赖
  "$GO_BIN" mod download
  
  # 构建到临时路径，后续比较是否变更
  NEW_BIN_PATH="/tmp/${BIN_NAME}.new.$$"
  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 "$GO_BIN" build \
    -ldflags="-w -s -X main.Version=$(date +%Y%m%d%H%M%S)" \
    -o "$NEW_BIN_PATH" \
    ./cmd/main.go
  
  if [[ $? -eq 0 ]]; then
    chmod +x "$NEW_BIN_PATH"
    log_success "后端构建完成"
  else
    log_error "后端构建失败"
    return 1
  fi
}

# 复制前端文件
copy_frontend_files() {
  log_info "复制前端文件..."
  
  if [[ -d "ui/dist" ]]; then
    # 检查 rsync 是否存在
    if command -v rsync &> /dev/null; then
      rsync -a --delete ui/dist/ "${INSTALL_DIR}/ui/dist/"
      log_success "使用 rsync 复制前端文件完成"
    else
      log_warning "rsync 未找到，使用 cp 命令"
      # 清空目标目录
      rm -rf "${INSTALL_DIR}/ui/dist"/*
      # 复制文件
      cp -r ui/dist/* "${INSTALL_DIR}/ui/dist/"
      log_success "使用 cp 复制前端文件完成"
    fi
  else
    log_warning "ui/dist 目录不存在，跳过复制"
  fi
}

# 比较文件哈希
hash_file() {
  local f="$1"
  [[ ! -f "$f" ]] && echo "" && return 0
  local h=$(sha256sum "$f" 2>/dev/null | awk '{print $1}')
  [[ -z "$h" ]] && h=$(md5sum "$f" 2>/dev/null | awk '{print $1}')
  echo "$h"
}

# 部署后端二进制，判断是否变更并标记
deploy_backend_binary() {
  BIN_CHANGED=false
  if [[ -n "${NEW_BIN_PATH:-}" && -f "$NEW_BIN_PATH" ]]; then
    local dst_bin="${INSTALL_DIR}/bin/${BIN_NAME}"
    local h_new=$(hash_file "$NEW_BIN_PATH")
    local h_old=$(hash_file "$dst_bin")
    if [[ "$h_new" != "$h_old" ]]; then
      install -m 0755 "$NEW_BIN_PATH" "$dst_bin"
      BIN_CHANGED=true
      log_info "后端二进制有变更，已更新到 ${dst_bin}"
    else
      log_info "后端二进制无变更，跳过更新"
    fi
    rm -f "$NEW_BIN_PATH"
  else
    log_info "未检测到新构建的后端二进制"
  fi
}

# 根据变更情况决定是否重启服务
maybe_restart_service() {
  if [[ "$NO_RESTART" == true ]]; then
    log_info "按参数要求不重启服务"
    return 0
  fi
  if [[ "$FORCE_RESTART" == true || "$BIN_CHANGED" == true || "$ENV_CHANGED" == true || "$SERVICE_CHANGED" == true ]]; then
    log_info "重启服务中... (原因: FORCE=${FORCE_RESTART}, BIN=${BIN_CHANGED}, ENV=${ENV_CHANGED}, SERVICE=${SERVICE_CHANGED})"
    if systemctl restart "${SERVICE_NAME}"; then
      log_success "服务重启成功"
    else
      log_error "服务重启失败"
      systemctl status "${SERVICE_NAME}" --no-pager
      return 1
    fi
  else
    log_info "无需重启服务"
  fi
  # 显示状态
  log_info "服务状态:"
  systemctl status "${SERVICE_NAME}" --no-pager --lines=5
}

# 处理环境变量
setup_environment() {
  log_info "设置环境变量..."
  
  local env_file="${INSTALL_DIR}/.env"
  local old_env_hash=""
  local new_env_hash=""
  if [[ -f "$env_file" ]]; then
    old_env_hash=$(sha256sum "$env_file" 2>/dev/null | awk '{print $1}')
    [[ -z "$old_env_hash" ]] && old_env_hash=$(md5sum "$env_file" 2>/dev/null | awk '{print $1}')
  fi
  
  if [[ "$NON_INTERACTIVE" == true ]]; then
    if [[ -n "${ENV_SRC}" && -f "${ENV_SRC}" ]]; then
      cp "${ENV_SRC}" "${env_file}"
      log_info "使用自定义环境文件: ${ENV_SRC}"
    elif [[ -f "${env_file}" ]]; then
      log_info "非交互模式，沿用已有环境文件"
    else
      log_error "非交互模式：未提供 --env 且不存在 ${env_file}，拒绝覆盖为示例配置"
      exit 1
    fi
  else
    if [[ -n "${ENV_SRC}" && -f "${ENV_SRC}" ]]; then
      cp "${ENV_SRC}" "${env_file}"
      log_info "使用自定义环境文件: ${ENV_SRC}"
    elif [[ -f ".env" ]]; then
      cp .env "${env_file}"
      log_info "使用项目 .env 文件"
    elif [[ -f ".env.example" ]]; then
      cp .env.example "${env_file}"
      log_info "使用 .env.example 模板"
    else
      log_warning "未找到环境文件，创建空文件"
      touch "${env_file}"
    fi
  fi
  
  # 应用覆盖
  if [[ ${#DECLARED_KV[@]} -gt 0 ]]; then
    log_info "应用环境变量覆盖..."
    tmp_env="${env_file}.tmp"
    cp "${env_file}" "$tmp_env"
    
    for kv in "${DECLARED_KV[@]}"; do
      key="${kv%%=*}"
      val="${kv#*=}"
      
      # 转义特殊字符
      val=$(echo "$val" | sed 's/[\/&]/\\&/g')
      
      if grep -qE "^${key}=" "$tmp_env"; then
        sed -i "s|^${key}=.*|${key}=${val}|" "$tmp_env"
        log_info "更新: ${key}=${val}"
      else
        echo "${key}=${val}" >> "$tmp_env"
        log_info "添加: ${key}=${val}"
      fi
    done
    
    mv "$tmp_env" "${env_file}"
  fi
  
  # 设置权限
  chown waveyo:waveyo "${env_file}"
  chmod 600 "${env_file}"

  # 计算新哈希
  if [[ -f "$env_file" ]]; then
    new_env_hash=$(sha256sum "$env_file" 2>/dev/null | awk '{print $1}')
    [[ -z "$new_env_hash" ]] && new_env_hash=$(md5sum "$env_file" 2>/dev/null | awk '{print $1}')
  fi
  if [[ "$old_env_hash" != "$new_env_hash" ]]; then
    ENV_CHANGED=true
    log_info "检测到 .env 变更（将考虑重启服务）"
  else
    ENV_CHANGED=false
  fi
  
  log_success "环境变量设置完成"
}

# 编辑配置文件
edit_config_file() {
  local env_file="${INSTALL_DIR}/.env"
  log_info "请编辑配置文件以确保数据库连接等参数正确"
  
  # 检测可用的文本编辑器
  local editor
  if command -v nano &> /dev/null; then
    editor="nano"
  elif command -v vim &> /dev/null; then
    editor="vim"
  elif command -v vi &> /dev/null; then
    editor="vi"
  else
    log_error "未找到可用的文本编辑器，请手动编辑 ${env_file}"
    return 1
  fi
  
  if [[ "$NON_INTERACTIVE" == true ]]; then
    log_info "非交互模式，跳过编辑配置"
    return 0
  fi
  log_info "使用 ${editor} 打开配置文件..."
  $editor "$env_file"
  
  read -p "配置文件已编辑完成，是否继续部署？(y/n) " -n 1 -r
  echo
  if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    log_info "部署已中止，请重新运行脚本继续"
    exit 0
  fi
}

# 安装 systemd 服务
setup_systemd_service() {
  log_info "安装 systemd 服务..."
  
  local service_src="scripts/systemd/${SERVICE_NAME}"
  
  # 检查服务文件
  if [[ ! -f "$service_src" ]]; then
    service_src="scripts/systemd/waveyo-koishi-mirror.service"
  fi
  
  if [[ ! -f "$service_src" ]]; then
    log_warning "未找到 systemd 服务文件，创建默认配置"
    
    cat > "/etc/systemd/system/${SERVICE_NAME}" << EOF
[Unit]
Description=${APP_NAME}
After=network.target
Wants=network.target

[Service]
Type=simple
User=waveyo
Group=waveyo
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=${INSTALL_DIR}/.env
ExecStart=${INSTALL_DIR}/bin/${BIN_NAME}
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
SyslogIdentifier=${APP_NAME}

# 安全设置
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${INSTALL_DIR}/logs ${INSTALL_DIR}/data

[Install]
WantedBy=multi-user.target
EOF
    
    log_info "创建默认 systemd 服务文件"
  else
    # 若目标存在，比较是否变更
    local target="/etc/systemd/system/${SERVICE_NAME}"
    if [[ -f "$target" ]]; then
      local src_hash=$(sha256sum "$service_src" 2>/dev/null | awk '{print $1}')
      [[ -z "$src_hash" ]] && src_hash=$(md5sum "$service_src" 2>/dev/null | awk '{print $1}')
      local dst_hash=$(sha256sum "$target" 2>/dev/null | awk '{print $1}')
      [[ -z "$dst_hash" ]] && dst_hash=$(md5sum "$target" 2>/dev/null | awk '{print $1}')
      if [[ "$src_hash" != "$dst_hash" ]]; then
        cp "$service_src" "$target"
        SERVICE_CHANGED=true
        log_info "systemd 服务文件有更新，已复制"
      else
        SERVICE_CHANGED=false
        log_info "systemd 服务文件无变更"
      fi
    else
      cp "$service_src" "$target"
      SERVICE_CHANGED=true
      log_info "首次复制 systemd 服务文件"
    fi
  fi
  
  # 重新加载 systemd
  systemctl daemon-reload
  
  # 启用服务
  systemctl enable "${SERVICE_NAME}"
  # 启动或保持运行状态，是否重启由后续逻辑决定
  if ! systemctl is-active --quiet "${SERVICE_NAME}"; then
    if systemctl start "${SERVICE_NAME}"; then
      log_success "服务已启动"
    else
      log_error "服务启动失败"
      systemctl status "${SERVICE_NAME}" --no-pager
      return 1
    fi
  fi
}

# 验证安装
verify_installation() {
  log_info "验证安装..."
  
  # 检查文件
  local checks=(
    "${INSTALL_DIR}/bin/${BIN_NAME}"
    "${INSTALL_DIR}/.env"
    "/etc/systemd/system/${SERVICE_NAME}"
  )
  
  for check in "${checks[@]}"; do
    if [[ -f "$check" ]]; then
      log_success "✓ $check"
    else
      log_error "✗ $check 未找到"
    fi
  done
  
  # 检查服务状态
  if systemctl is-active --quiet "${SERVICE_NAME}"; then
    log_success "✓ 服务正在运行"
  else
    log_error "✗ 服务未运行"
    systemctl status "${SERVICE_NAME}" --no-pager
  fi
  
  # 检查端口监听
  if command -v ss &> /dev/null; then
    local api_port=$(grep -E "^API_PORT=" "${INSTALL_DIR}/.env" | cut -d'=' -f2)
    api_port=${api_port:-8080}
    
    if ss -tlnp | grep -q ":${api_port} "; then
      log_success "✓ API 端口 ${api_port} 正在监听"
    else
      log_warning "⚠ API 端口 ${api_port} 未监听"
    fi
  fi
  
  # 检查进程
  if pgrep -f "${BIN_NAME}" > /dev/null; then
    log_success "✓ 进程正在运行"
  else
    log_error "✗ 进程未找到"
  fi
  
  log_success "验证完成"
}

# 显示部署信息
show_deployment_info() {
  local api_port=$(grep -E "^API_PORT=" "${INSTALL_DIR}/.env" | cut -d'=' -f2)
  api_port=${api_port:-8080}
  
  local log_level=$(grep -E "^LOG_LEVEL=" "${INSTALL_DIR}/.env" | cut -d'=' -f2)
  log_level=${log_level:-info}
  
  cat << EOF

${GREEN}════════════════════════════════════════════════════════════════════════════════${NC}
${GREEN}                          ${APP_NAME} 部署完成！                          ${NC}
${GREEN}════════════════════════════════════════════════════════════════════════════════${NC}

${BLUE}部署信息:${NC}
  • 安装目录: ${INSTALL_DIR}
  • 服务名称: ${SERVICE_NAME}
  • 运行用户: waveyo
  • API 端口: ${api_port}
  • 日志级别: ${log_level}

${BLUE}常用命令:${NC}
  # 查看服务状态
  sudo systemctl status ${SERVICE_NAME}
  
  # 查看服务日志
  sudo journalctl -u ${SERVICE_NAME} -f
  
  # 重启服务
  sudo systemctl restart ${SERVICE_NAME}
  
  # 停止服务
  sudo systemctl stop ${SERVICE_NAME}
  
  # 启动服务
  sudo systemctl start ${SERVICE_NAME}

${BLUE}文件位置:${NC}
  • 配置文件: ${INSTALL_DIR}/.env
  • 日志文件: ${INSTALL_DIR}/logs/
  • 数据文件: ${INSTALL_DIR}/data/
  • 二进制文件: ${INSTALL_DIR}/bin/${BIN_NAME}

${BLUE}下一步:${NC}
  1. 检查服务状态: sudo systemctl status ${SERVICE_NAME}
  2. 查看日志: sudo journalctl -u ${SERVICE_NAME} -f
  3. 访问 API: curl http://localhost:${api_port}/health

${GREEN}════════════════════════════════════════════════════════════════════════════════${NC}

EOF
}

# 清理临时文件
cleanup() {
  log_info "清理临时文件..."
  
  # 清理 npm 缓存（可选）
  # "$PM_BIN" cache clean --force
  
  # 清理 Go 构建缓存
  # "$GO_BIN" clean -cache
  
  log_success "清理完成"
}

# 主函数
main() {
  log_info "开始部署 ${APP_NAME}..."
  
  # 检查 root 权限
  check_root "$@"
  
  # 设置 PATH
  setup_path
  
  # 检查依赖（这会调用 check_system_deps）
  check_dependencies
  
  # 获取二进制路径
  get_bin_paths
  
  # 创建用户和目录
  setup_user_and_dirs
  
  # 构建（如果不跳过）
  if [[ "$SKIP_BUILD" != true ]]; then
    build_frontend
    build_backend
    copy_frontend_files
    deploy_backend_binary
  else
    log_info "跳过所有构建步骤"
  fi
  
  # 设置环境变量
  setup_environment

  # 编辑配置文件
  edit_config_file
  
  # 安装 systemd 服务
  setup_systemd_service
  
  # 根据变更情况重启服务
  maybe_restart_service
  
  # 验证安装
  verify_installation
  
  # 清理
  cleanup
  
  # 显示部署信息
  show_deployment_info
  
  log_success "${APP_NAME} 部署完成！"
}

# 异常处理
trap 'log_error "部署过程中断"; exit 1' INT TERM

# 运行主函数
main "$@"

exit 0

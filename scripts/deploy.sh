#!/usr/bin/env bash

set -euo pipefail

APP_NAME="WaveYo-Koishi-Mirror"
INSTALL_DIR="/opt/waveyo-koishi-mirror"
BIN_NAME="koishi-mirror"
SERVICE_NAME="waveyo-koishi-mirror.service"

usage() {
  cat <<EOF
用法: $0 [--env /path/to/.env] [--set KEY=VALUE ...] [--service-name NAME]

选项:
  --env PATH           指定要部署的 .env 文件路径
  --set KEY=VALUE      在部署前修改/覆盖 .env 中的键值，多次使用支持多个键
  --service-name NAME  自定义 systemd 服务名 (默认: ${SERVICE_NAME})

示例:
  sudo bash scripts/deploy.sh --env ./prod.env --set API_PORT=8080 --set LOG_LEVEL=info
EOF
}

ENV_SRC=""
DECLARED_KV=()

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env)
      ENV_SRC="$2"; shift 2;;
    --set)
      DECLARED_KV+=("$2"); shift 2;;
    --service-name)
      SERVICE_NAME="$2"; shift 2;;
    -h|--help)
      usage; exit 0;;
    *)
      echo "未知参数: $1"; usage; exit 1;;
  esac
done

need_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "错误: 未找到命令 $1"; exit 1; }; }

echo "检查依赖..."
need_cmd go
need_cmd npm
need_cmd systemctl
need_cmd useradd || true
need_cmd id

echo "创建系统用户/目录..."
if ! id -u waveyo >/dev/null 2>&1; then
  useradd --system --create-home --home-dir /home/waveyo --shell /usr/sbin/nologin waveyo || true
fi
mkdir -p "${INSTALL_DIR}/bin" "${INSTALL_DIR}/ui/dist"

echo "构建前端..."
pushd ui >/dev/null
npm install
npm run build
popd >/dev/null

echo "复制前端构建产物..."
rsync -a ui/dist/ "${INSTALL_DIR}/ui/dist/"

echo "构建后端..."
GOOS=linux GOARCH=amd64 go build -o "${INSTALL_DIR}/bin/${BIN_NAME}" ./cmd/main.go
chown -R waveyo:waveyo "${INSTALL_DIR}"

echo "准备 .env ..."
if [[ -n "${ENV_SRC}" ]]; then
  cp "${ENV_SRC}" "${INSTALL_DIR}/.env"
else
  # 复制仓库根的 .env 或 .env.example
  if [[ -f .env ]]; then
    cp .env "${INSTALL_DIR}/.env"
  else
    cp .env.example "${INSTALL_DIR}/.env"
  fi
fi

# 应用 --set 覆盖项
if [[ ${#DECLARED_KV[@]} -gt 0 ]]; then
  tmp_env="${INSTALL_DIR}/.env.tmp"
  cp "${INSTALL_DIR}/.env" "$tmp_env"
  for kv in "${DECLARED_KV[@]}"; do
    key="${kv%%=*}"; val="${kv#*=}"
    # 若存在则替换，否则追加
    if grep -qE "^${key}=" "$tmp_env"; then
      sed -i "s|^${key}=.*|${key}=${val}|" "$tmp_env"
    else
      echo "${key}=${val}" >> "$tmp_env"
    fi
  done
  mv "$tmp_env" "${INSTALL_DIR}/.env"
fi

echo "安装 systemd 服务..."
SERVICE_SRC="scripts/systemd/${SERVICE_NAME}"
if [[ ! -f "$SERVICE_SRC" ]]; then
  # 兼容默认文件名
  SERVICE_SRC="scripts/systemd/waveyo-koishi-mirror.service"
fi
cp "$SERVICE_SRC" "/etc/systemd/system/${SERVICE_NAME}"
systemctl daemon-reload
systemctl enable "${SERVICE_NAME}"
systemctl restart "${SERVICE_NAME}"

echo "完成: 服务 ${SERVICE_NAME} 已启动，工作目录 ${INSTALL_DIR}"


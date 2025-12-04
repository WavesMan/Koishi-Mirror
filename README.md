# Koishi Plugins 镜像仓库

加速你的 Koishi Bot 开发流程。Koishi Plugins 的镜像仓库，提供高效、稳定的包同步和下载服务。
**让 Koishi Plugins 都飞起来。**

## 功能特点

- **定向加速**：专为 Koishi 生态设计，从指定数据源定向拉取插件包
- **极速下载**：支持 S3/OSS 预签名直链下载（302 Redirect），充分利用对象存储带宽
- **增量同步**：智能检测新增/更新的包，避免重复下载，确保插件版本最新
- **Web 可视化**：提供友好的 Web 界面，实时查看镜像状态、插件列表和存储统计
- **npm 兼容**：完全兼容标准 npm/yarn/pnpm 客户端，无缝切换

## 系统架构

- **后端**：Golang + Gin 框架，提供 API 接口、静态服务及同步任务调度
- **前端**：Vue 3 + Vite，提供现代化的 Web 管理界面
- **存储**：支持 AWS S3、阿里云 OSS、腾讯云 COS 等兼容 S3 协议的存储服务

## 快速开始

### 1. 环境要求

- Go 1.22+
- Node.js 21+
- S3 兼容的存储服务（AWS S3, MinIO, Aliyun OSS 等）
- PostgreSQL (用于元数据存储)

### 2. 后端部署

后端代码位于项目根目录。

```bash
# 复制环境变量配置文件
cp .env.example .env

# 编辑环境变量配置
# 请根据实际情况修改 .env 文件中的配置，包括 S3 和数据库配置

# 编译
go build -o npm-mirror ./cmd/main.go

# 运行
./npm-mirror
```

### 3. 前端构建

前端代码位于 `ui` 目录。后端服务会自动托管构建好的前端静态资源。

```bash
# 进入前端目录
cd ui

# 安装依赖
npm install

# 构建
npm run build

# 构建产物将生成在 ui/dist 目录，后端启动时会自动挂载此目录到根路径
```

### 4. Docker 部署 (推荐)

提供了 `Dockerfile` 和 `docker-compose.yml`，可一键启动服务。

1. **配置环境变量**：
   创建 `.env` 文件（参考 `.env.example`），填写 S3 等配置信息。

2. **启动服务**：
   ```bash
   docker-compose up -d
   ```

   该命令会自动启动 `npm-mirror` 和 `postgres` 数据库服务。

## 配置说明

### 环境变量

通过 `.env` 文件进行配置：

- `S3_ENDPOINT`：S3 服务端点
- `S3_ACCESS_KEY`：S3 访问密钥
- `S3_SECRET_KEY`：S3 密钥
- `S3_BUCKET`：S3 存储桶名称
- `DATA_SOURCE_URL`：上游数据源 URL (例如 https://ks-store.waveyo.cn/index.json)
- `SYNC_INTERVAL`：同步间隔（如 1h）
- `API_PORT`：服务端口 (默认 8080)
- `DB_DSN`: PostgreSQL 连接字符串 (优先)
- `PG_HOST` / `PG_PORT` / `PG_USER` / `PG_PASSWORD` / `PG_DB` / `PG_SSLMODE`: PostgreSQL 连接参数 (作为 DB_DSN 的备选)

## 使用方法

### 通过 npm 命令使用

```bash
# 安装单个插件
npm install <package-name> --registry=http://<your-server-ip>:8080

# 设置为项目默认源
npm config set registry http://<your-server-ip>:8080
```

### Web 管理界面

启动服务后，直接访问 `http://<your-server-ip>:8080` 即可打开 Web 界面。

## 项目结构

```
waveyo-npm-mirror/
├── cmd/              # 程序入口
├── config/           # 配置加载
├── internal/         # 核心业务逻辑
│   ├── api/          # HTTP 接口与静态服务
│   ├── s3client/     # S3 客户端封装
│   ├── sync/         # 同步引擎
│   └── storage/      # 数据库存储层
├── ui/               # 前端源代码 (Vue 3)
└── README.md         # 项目说明
```

## 许可证

MIT

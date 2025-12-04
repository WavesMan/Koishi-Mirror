#!/bin/bash

# 遇到错误立即退出
set -e

# 检查 docker 是否安装
if ! command -v docker &> /dev/null; then
    echo "错误: 未找到 docker 命令。请先安装 Docker。"
    exit 1
fi

# 设置镜像名称
IMAGE_NAME="wavesman/koishi-mirror"
TAG="latest"

# 询问版本号
read -p "请输入版本号 (例如: v1.0.0): " VERSION

# 登录 Docker Hub (如果需要)
# docker login

# 构建镜像并打上两个标签
echo "正在构建 Docker 镜像..."
if ! docker build -t $IMAGE_NAME:$VERSION -t $IMAGE_NAME:$TAG .; then
    echo "错误: 镜像构建失败"
    exit 1
fi

# 推送版本号标签
echo "正在推送版本标签: $VERSION ..."
if ! docker push $IMAGE_NAME:$VERSION; then
    echo "错误: 版本标签推送失败"
    exit 1
fi

# 推送 latest 标签
echo "正在推送 latest 标签..."
if ! docker push $IMAGE_NAME:$TAG; then
    echo "错误: latest 标签推送失败"
    exit 1
fi

echo "完成！镜像已发布为:"
echo "  - $IMAGE_NAME:$VERSION"
echo "  - $IMAGE_NAME:$TAG"

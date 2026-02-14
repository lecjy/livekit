#!/usr/bin/env bash
# Copyright 2023 LiveKit, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# LiveKit install script for Linux

# 设置shell选项：使用未定义的变量时立即退出
set -u
# 设置错误追踪：使ERR陷阱在函数和子shell中生效
set -o errtrace
# 设置errexit选项：当命令返回非零状态时立即退出
set -o errexit
# 设置pipefail选项：管道命令中只要有一个命令失败，整个管道就失败
set -o pipefail

# 定义仓库名称变量
REPO="livekit"
# 定义安装路径变量
INSTALL_PATH="/usr/local/bin"

# 定义日志函数，用于格式化输出信息
log()  { printf "%b\n" "$*"; }
# 定义终止函数，用于输出错误信息并退出脚本
abort() {
  printf "%s\n" "$@" >&2
  exit 1
}

# 定义函数：根据GitHub获取最新版本号
# i.e. 1.0.0
get_latest_version()
{
  # 使用curl获取GitHub API的最新发布信息，并用grep提取版本号
  latest_version=$(curl -s https://api.github.com/repos/livekit/$REPO/releases/latest | grep -oP '"tarball_url": ".*/tarball/v\K([^/]*)(?=")')
  # 输出版本号
  printf "%s" "$latest_version"
}

# 检查是否使用bash执行脚本
if [ -z "${BASH_VERSION:-}" ]
then
  # 如果不是bash则终止脚本
  abort "This script requires bash"
fi

# 检查安装路径是否存在
if [ ! -d ${INSTALL_PATH} ]
then
  # 如果安装路径不存在则终止脚本
  abort "Could not install, ${INSTALL_PATH} doesn't exist"
fi

# 初始化sudo前缀变量为空
SUDO_PREFIX=""
# 检查是否有写入安装路径的权限
if [ ! -w ${INSTALL_PATH} ]
then
  # 如果没有写入权限则使用sudo
  SUDO_PREFIX="sudo"
  # 提示用户需要sudo权限
  log "sudo is required to install to ${INSTALL_PATH}"
fi

# 检查系统是否安装了curl命令
if ! command -v curl >/dev/null
then
  # 如果没有curl则终止脚本
  abort "cURL is required and is not found"
fi

# 获取操作系统类型
OS="$(uname)"
# 检查操作系统类型
if [[ "${OS}" == "Darwin" ]]
then
  # 如果是MacOS则提示使用Homebrew安装并终止
  abort "Installer not supported on MacOS, please install using Homebrew."
elif [[ "${OS}" != "Linux" ]]
then
  # 如果不是Linux系统则终止脚本
  abort "Installer is only supported on Linux."
fi

# 获取系统架构
ARCH="$(uname -m)"

# 根据Linux系统的架构名称进行转换
if [[ "${ARCH}" == "aarch64" ]]
then
  # 将aarch64转换为arm64
  ARCH="arm64"
elif [[ "${ARCH}" == "x86_64" ]]
then
  # 将x86_64转换为amd64
  ARCH="amd64"
fi

# 调用函数获取最新版本号
VERSION=$(get_latest_version)
# 构建下载URL
ARCHIVE_URL="https://github.com/livekit/$REPO/releases/download/v${VERSION}/${REPO}_${VERSION}_linux_${ARCH}.tar.gz"

# 检查版本号是否符合SemVer规范（主版本.次版本.修订版本）
if ! [[ "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]
then
  # 如果版本号格式不正确则终止脚本
  abort "Invalid version: ${VERSION}"
fi

# 输出安装信息和版本号
log "Installing ${REPO} ${VERSION}"
# 输出下载地址信息
log "Downloading from ${ARCHIVE_URL}..."

# 使用curl下载文件并通过管道传递给tar解压
curl -s -L "${ARCHIVE_URL}" | ${SUDO_PREFIX} tar xzf - -C "${INSTALL_PATH}" --wildcards --no-anchored "$REPO*"

# 输出安装完成信息
log "\nlivekit-server is installed to $INSTALL_PATH\n"
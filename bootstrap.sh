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


# 第一段：检查并安装 mage 工具，command -v mage: 检查系统中是否存在 mage 命令，&> /dev/null: 将标准输出和错误输出都重定向到空设备（不显示任何输出），!取反，所以条件成立时表示 mage 命令不存在
if ! command -v mage &> /dev/null
then
  # 如果 mage 不存在，进入安装流程

  # pushd 切换到 /tmp 目录，并将当前目录保存到目录栈，之后可以用 popd 返回原目录
  pushd /tmp

  git clone https://github.com/magefile/mage
  cd mage
  # 运行 mage 自带的安装脚本 bootstrap.go，这个脚本会编译并安装 mage 到 $GOPATH/bin 目录
  go run bootstrap.go
  rm -rf /tmp/mage
  # popd 返回之前保存的目录（执行 pushd 之前的目录）
  popd
fi

# 第二段：再次验证 mage是否可用
if ! command -v mage &> /dev/null
then
  # 如果仍然找不到 mage 命令

  # 输出提示信息，告诉用户需要将 Go 的 bin 目录添加到 PATH 环境变量中，`go env GOPATH`: 反引号执行命令，获取 Go 的 GOPATH 路径，\$PATH: 反斜杠转义 $，避免被 shell 解释为变量，输出实际的 $PATH 字符串
  echo "Ensure `go env GOPATH`/bin is in your \$PATH"

  # 退出脚本，返回错误码 1 表示失败
  exit 1
fi

# 第三段：下载 Go 依赖，下载当前项目 go.mod 文件中定义的所有依赖，这些依赖会被下载到 Go module 缓存中（$GOPATH/pkg/mod）
go mod download

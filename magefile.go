// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build mage
// +build mage

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/magefile/mage/mg"

	"github.com/livekit/livekit-server/version"
	"github.com/livekit/mageutil"
	_ "github.com/livekit/psrpc"
)

// lecjy 使用Mage构建工具的构建脚本，用于LiveKit Server项目的自动化构建、测试和发布

const (
	// lecjy 用于跟踪文件变更的校验和文件
	goChecksumFile = ".checksumgo"
	// lecjy Docker镜像名称
	imageName      = "livekit/livekit-server"
)

// Default target to run when none is specified
// If not set, running mage will list available targets
var (
	// lecjy 默认任务：执行Build
	Default     = Build

	// lecjy 文件变更检测器，监控当前目录下的.go和.mod文件
	checksummer = mageutil.NewChecksummer(".", goChecksumFile, ".go", ".mod")
)

func init() {
	// lecjy 设置忽略路径，这些文件的变更不会触发重新构建
	checksummer.IgnoredPaths = []string{
		"pkg/service/wire_gen.go",// lecjy Wire生成的代码，忽略它
		"pkg/rtc/types/typesfakes",// lecjy 测试用的fake实现，忽略
	}
}

// explicitly reinstall all deps
func Deps() error {
	// lecjy 显式重新安装所有依赖，参数true表示强制重新安装
	return installTools(true)
}

// builds LiveKit server
// lecjy 构建 LiveKit 服务器
func Build() error {
	// lecjy 确保 generateWire 任务先执行（依赖管理）
	mg.Deps(generateWire)

	// lecjy 检查文件是否有变更
	if !checksummer.IsChanged() {
		// lecjy 如果没有变更，提示已是最新
		fmt.Println("up to date")
		// lecjy 直接返回，不执行构建
		return nil
	}

	fmt.Println("building...")

	// lecjy 创建 bin 目录，权限 0755（rwxr-xr-x）
	if err := os.MkdirAll("bin", 0755); err != nil {
		// lecjy 如果创建目录失败，返回错误
		return err
	}

	// lecjy 创建在 cmd/server 目录下执行 go build 命令的命令对象
	if err := mageutil.RunDir(context.Background(), "cmd/server", "go build -o ../../bin/livekit-server"); err != nil {
		// lecjy 如果构建失败，返回错误
		return err
	}
	// lecjy 构建成功后，写入新的校验和
	checksummer.WriteChecksum()

	// lecjy 返回 nil 表示成功
	return nil
}

// builds binary that runs on linux
// lecjy 构建能在 Linux 上运行的二进制文件
func BuildLinux() error {
	mg.Deps(generateWire)
	if !checksummer.IsChanged() {
		fmt.Println("up to date")
		return nil
	}

	fmt.Println("building...")
	if err := os.MkdirAll("bin", 0755); err != nil {
		return err
	}

	// lecjy 获取目标架构，如果没设置环境变量，默认 amd64
	buildArch := os.Getenv("GOARCH")
	if len(buildArch) == 0 {
		buildArch = "amd64"
	}

	cmd := mageutil.CommandDir(context.Background(), "cmd/server", "go build -buildvcs=false -o ../../bin/livekit-server-" + buildArch)

	// lecjy 设置环境变量，实现交叉编译
	cmd.Env = []string{
		"GOOS=linux", // lecjy 目标操作系统为 Linux
		"GOARCH=" + buildArch, // lecjy 目标架构
		"HOME=" + os.Getenv("HOME"), // lecjy 传递 HOME 环境变量
		"GOPATH=" + os.Getenv("GOPATH"), // lecjy 传递 GOPATH 环境变量
	}

	// lecjy 执行命令
	if err := cmd.Run(); err != nil {
		return err
	}

	checksummer.WriteChecksum()
	return nil
}

// lecjy 死锁检测工具注入
func Deadlock() error {
	ctx := context.Background()

	// lecjy 安装 goimports 工具
	if err := mageutil.InstallTool("golang.org/x/tools/cmd/goimports", "latest", false); err != nil {
		return err
	}

	// lecjy 获取 go-deadlock 包
	if err := mageutil.Run(ctx, "go get github.com/sasha-s/go-deadlock"); err != nil {
		return err
	}

	// lecjy 将所有 sync.Mutex 替换为 deadlock.Mutex，grep 查找包含 sync.Mutex 的文件，xargs 传递给 sed 进行替换
	if err := mageutil.Pipe("grep -rl sync.Mutex ./pkg", "xargs sed -i  -e s/sync.Mutex/deadlock.Mutex/g"); err != nil {
		return err
	}

	// lecjy 将所有 sync.RWMutex 替换为 deadlock.RWMutex
	if err := mageutil.Pipe("grep -rl sync.RWMutex ./pkg", "xargs sed -i  -e s/sync.RWMutex/deadlock.RWMutex/g"); err != nil {
		return err
	}

	// lecjy 对修改后的文件运行 goimports 格式化导入
	if err := mageutil.Pipe("grep -rl deadlock.Mutex\\|deadlock.RWMutex ./pkg", "xargs goimports -w"); err != nil {
		return err
	}

	// lecjy 整理 go module
	if err := mageutil.Run(ctx, "go mod tidy"); err != nil {
		return err
	}
	return nil
}

// lecjy 恢复标准互斥锁
func Sync() error {
	// lecjy 将 deadlock.Mutex 替换回 sync.Mutex
	if err := mageutil.Pipe("grep -rl deadlock.Mutex ./pkg", "xargs sed -i  -e s/deadlock.Mutex/sync.Mutex/g"); err != nil {
		return err
	}
	if err := mageutil.Pipe("grep -rl deadlock.RWMutex ./pkg", "xargs sed -i  -e s/deadlock.RWMutex/sync.RWMutex/g"); err != nil {
		return err
	}
	if err := mageutil.Pipe("grep -rl sync.Mutex\\|sync.RWMutex ./pkg", "xargs goimports -w"); err != nil {
		return err
	}
	if err := mageutil.Run(context.Background(), "go mod tidy"); err != nil {
		return err
	}
	return nil
}

// builds and publish snapshot docker image
// lecjy 构建并发布快照版本的 Docker 镜像
func PublishDocker() error {
	// don't publish snapshot versions as latest or minor version
	// lecjy 检查是否是快照版本，如果不是则拒绝发布
	if !strings.Contains(version.Version, "SNAPSHOT") {
		return errors.New("Cannot publish non-snapshot versions")
	}
    // lecjy 构建带版本号的镜像标签
	versionImg := fmt.Sprintf("%s:v%s", imageName, version.Version)

	// lecjy 创建 docker buildx 命令
	cmd := exec.Command("docker", "buildx", "build",
		"--push", "--platform", "linux/amd64,linux/arm64",// lecjy 参数分别是：推送镜像到仓库，多架构支持
		"--tag", versionImg, // lecjy 设置镜像标签
		".")// lecjy 使用当前目录的 Dockerfile
	mageutil.ConnectStd(cmd) // lecjy 连接标准输入输出，实时显示日志
	if err := cmd.Run(); err != nil {
		return err
	}
	return nil
}

// run unit tests, skipping integration
// lecjy 运行单元测试，跳过集成测试
func Test() error {
	// lecjy 先执行 wire 生成和设置文件描述符限制
	mg.Deps(generateWire, setULimit)
	// lecjy -short 跳过集成测试
	return mageutil.Run(context.Background(), "go test -short ./... -count=1")
}

// run all tests including integration
// lecjy 运行所有测试，包括集成测试
func TestAll() error {
	mg.Deps(generateWire, setULimit)
	// lecjy 4分钟超时，详细输出
	return mageutil.Run(context.Background(), "go test ./... -count=1 -timeout=4m -v")
}

// cleans up builds
// lecjy 清理构建产物
func Clean() {
	fmt.Println("cleaning...")
	os.RemoveAll("bin")// lecjy 删除 bin 目录及其所有内容
	os.Remove(goChecksumFile)// lecjy 删除校验和文件
}

// regenerate code
// lecjy 重新生成代码
func Generate() error {
	mg.Deps(installDeps, generateWire)

	fmt.Println("generating...")
	// lecjy 执行所有 go generate 
	return mageutil.Run(context.Background(), "go generate ./...")
}

// code generation for wiring
// lecjy 依赖注入代码生成
func generateWire() error {
	// lecjy 确保依赖已安装
	mg.Deps(installDeps)
	if !checksummer.IsChanged() {
		// lecjy 如果没有变更，跳过
		return nil
	}

	fmt.Println("wiring...")
    // lecjy 获取 wire 工具的路径
	wire, err := mageutil.GetToolPath("wire")
	if err != nil {
		return err
	}

	// lecjy 在 pkg/service 目录下执行 wire 命令
	cmd := exec.Command(wire)
	cmd.Dir = "pkg/service"
	// lecjy 连接标准输入输出
	mageutil.ConnectStd(cmd)
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

// implicitly install deps
// lecjy 隐式安装依赖
func installDeps() error {
	// lecjy 不强制重新安装
	return installTools(false)
}

func installTools(force bool) error {
	// lecjy 定义需要安装的工具及其版本
	tools := map[string]string{
		"github.com/google/wire/cmd/wire": "latest",// lecjy Wire 依赖注入工具
	}
	// lecjy 遍历安装每个工具
	for t, v := range tools {
		if err := mageutil.InstallTool(t, v, force); err != nil {
			return err
		}
	}
	return nil
}

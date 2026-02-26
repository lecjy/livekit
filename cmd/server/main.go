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

package main

// main.go - LiveKit 服务器的主入口文件，这个文件定义了命令行接口、配置加载和服务器启动逻辑

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/signal"
	"runtime"      // 与Go运行时交互，如GC、性能分析
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/urfave/cli/v3" // 第三方命令行应用框架

	"github.com/livekit/protocol/logger"          // LiveKit的日志系统
	"github.com/livekit/protocol/tracer/jaeger"   // Jaeger分布式追踪集成

	"github.com/livekit/livekit-server/pkg/config"   // 配置管理
	"github.com/livekit/livekit-server/pkg/routing"  // 节点路由
	"github.com/livekit/livekit-server/pkg/rtc"      // WebRTC核心功能
	"github.com/livekit/livekit-server/pkg/service"  // 服务层
	"github.com/livekit/livekit-server/pkg/telemetry/prometheus" // Prometheus监控
	"github.com/livekit/livekit-server/version"
)

// baseFlags 定义了所有基础命令行标志，这些标志是所有子命令都可能需要的通用配置
var baseFlags = []cli.Flag{
	// 监听地址，可以多次使用来监听多个IP
	&cli.StringSliceFlag{
		Name:  "bind",
		Usage: "IP address to listen on, use flag multiple times to specify multiple addresses",
	},
	// 配置文件路径
	&cli.StringFlag{
		Name:  "config",
		Usage: "path to LiveKit config file",
	},
	// 直接通过YAML字符串传递配置，通常用于容器环境
	&cli.StringFlag{
		Name:    "config-body",
		Usage:   "LiveKit config in YAML, typically passed in as an environment var in a container",
		Sources: cli.EnvVars("LIVEKIT_CONFIG"),
	},
	// API密钥文件路径
	&cli.StringFlag{
		Name:  "key-file",
		Usage: "path to file that contains API keys/secrets",
	},
	// 直接传递API密钥，支持多行格式 "key1: secret1\nkey2: secret2"
	&cli.StringFlag{
		Name:    "keys",
		Usage:   "api keys (key: secret\\n)",
		Sources: cli.EnvVars("LIVEKIT_KEYS"),
	},
	// 节点所在的区域，用于区域感知的节点选择
	&cli.StringFlag{
		Name:    "region",
		Usage:   "region of the current node. Used by regionaware node selector",
		Sources: cli.EnvVars("LIVEKIT_REGION"),
	},
	// 节点IP地址，默认自动检测，可以手动指定
	&cli.StringFlag{
		Name:    "node-ip",
		Usage:   "IP address of the current node, used to advertise to clients. Automatically determined by default",
		Sources: cli.EnvVars("NODE_IP"),
	},
	// WebRTC使用的UDP端口
	&cli.StringFlag{
		Name:    "udp-port",
		Usage:   "UDP port(s) to use for WebRTC traffic",
		Sources: cli.EnvVars("UDP_PORT"),
	},
	// Redis主机地址
	&cli.StringFlag{
		Name:    "redis-host",
		Usage:   "host (incl. port) to redis server",
		Sources: cli.EnvVars("REDIS_HOST"),
	},
	// Redis密码
	&cli.StringFlag{
		Name:    "redis-password",
		Usage:   "password to redis",
		Sources: cli.EnvVars("REDIS_PASSWORD"),
	},
	// TURN服务器的TLS证书文件
	&cli.StringFlag{
		Name:    "turn-cert",
		Usage:   "tls cert file for TURN server",
		Sources: cli.EnvVars("LIVEKIT_TURN_CERT"),
	},
	// TURN服务器的TLS密钥文件
	&cli.StringFlag{
		Name:    "turn-key",
		Usage:   "tls key file for TURN server",
		Sources: cli.EnvVars("LIVEKIT_TURN_KEY"),
	},
	// CPU性能分析输出文件
	&cli.StringFlag{
		Name:  "cpuprofile",
		Usage: "write CPU profile to `file`",
	},
	// 内存性能分析输出文件
	&cli.StringFlag{
		Name:  "memprofile",
		Usage: "write memory profile to `file`",
	},
	// 开发模式标志：启用调试日志、控制台格式、pprof调试端点
	&cli.BoolFlag{
		Name:  "dev",
		Usage: "sets log-level to debug, console formatter, and /debug/pprof. insecure for production",
	},
	// 禁用严格配置解析模式（隐藏选项，用于特殊情况）
	&cli.BoolFlag{
		Name:   "disable-strict-config",
		Usage:  "disables strict config parsing",
		Hidden: true,
	},
}

// init 包初始化函数
func init() {
	// 设置随机数种子，确保每次运行生成的随机值不同
	rand.Seed(time.Now().Unix())
}

func main() {
	// 延迟执行的恢复函数，用于处理panic，如果发生panic且被恢复，程序会以错误码1退出
	defer func() {
		if rtc.Recover(logger.GetLogger()) != nil {
			os.Exit(1)
		}
	}()

	// 生成所有配置标志，包括基础标志和从配置结构生成的标志
	generatedFlags, err := config.GenerateCLIFlags(baseFlags, true)
	if err != nil {
		fmt.Println(err)
	}

	// 定义根命令
	cmd := &cli.Command{
		Name:        "livekit-server",
		Usage:       "High performance WebRTC server",
		Description: "run without subcommands to start the server",
		// 合并基础标志和生成的标志
		Flags:  append(baseFlags, generatedFlags...),
		// 默认操作：启动服务器
		Action: startServer,
		// 子命令定义
		Commands: []*cli.Command{
			{
				Name:   "generate-keys",
				Usage:  "generates an API key and secret pair",
				Action: generateKeys,
			},
			{
				Name:   "ports",
				Usage:  "print ports that server is configured to use",
				Action: printPorts,
			},
			{
				// 这个子命令已弃用，令牌生成功能已移至CLI
				Name:   "create-join-token",
				Hidden: true,
				Usage:  "create a room join token for development use",
				Action: createToken,
				Flags: []cli.Flag{ 	// 子命令的标志
					&cli.StringFlag{
						Name:     "room",
						Usage:    "name of room to join",
						Required: true,
					},
					&cli.StringFlag{
						Name:     "identity",
						Usage:    "identity of participant that holds the token",
						Required: true,
					},
					&cli.BoolFlag{
						Name:     "recorder",
						Usage:    "creates a hidden participant that can only subscribe",
						Required: false,
					},
				},
			},
			{
				Name:   "list-nodes",
				Usage:  "list all nodes",
				Action: listNodes,
			},
			{
				Name:   "help-verbose",
				Usage:  "prints app help, including all generated configuration flags",
				Action: helpVerbose,
			},
		},
		// 设置版本号
		Version: version.Version,
	}

	// 执行命令
	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Println(err)
	}
}

// getConfig 从命令行参数和配置文件加载配置
// 返回配置对象或错误
func getConfig(c *cli.Command) (*config.Config, error) {
	// 获取配置字符串（从文件或直接传递的YAML）
	confString, err := getConfigString(c.String("config"), c.String("config-body"))
	if err != nil {
		return nil, err
	}

	// 是否启用严格模式（默认启用）
	strictMode := true
	if c.Bool("disable-strict-config") {
		strictMode = false
	}

	// 创建新的配置对象
	conf, err := config.NewConfig(confString, strictMode, c, baseFlags)
	if err != nil {
		return nil, err
	}
	// 初始化日志记录器
	config.InitLoggerFromConfig(&conf.Logging)

	// 开发模式下的特殊处理
	if conf.Development {
		logger.Infow("starting in development mode")

		// 如果没有配置密钥，使用开发密钥
		if len(conf.Keys) == 0 {
			logger.Infow("no keys provided, using placeholder keys",
				"API Key", "devkey",
				"API Secret", "secret",
			)
			conf.Keys = map[string]string{
				"devkey": "secret",
			}
			// 开发模式下决定是否要将 WebRTC 的 IP 地址限制在与绑定地址相同的子网中
			shouldMatchRTCIP := false
			// 开发模式下默认绑定到本地地址
			if conf.BindAddresses == nil {
				conf.BindAddresses = []string{
					"127.0.0.1",
					"::1",
				}
			} else {
				// 检查是否有非回环地址
				for _, addr := range conf.BindAddresses {
					ip := net.ParseIP(addr)
					if ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
						shouldMatchRTCIP = true
					}
				}
			}
			// 如果有非回环地址，配置RTC IP过滤规则
			if shouldMatchRTCIP {
				for _, bindAddr := range conf.BindAddresses {
					conf.RTC.IPs.Includes = append(conf.RTC.IPs.Includes, bindAddr+"/24")
				}
			}
		}
	}
	return conf, nil
}

// startServer 启动LiveKit服务器，这是默认命令的执行函数
func startServer(ctx context.Context, c *cli.Command) error {
	// 加载配置
	conf, err := getConfig(c)
	if err != nil {
		return err
	}
	
	// 配置Jaeger分布式追踪
	if url := conf.Trace.JaegerURL; url != "" {
		jaeger.Configure(ctx, url, "livekit")
	}

	// 验证API密钥长度是否有效
	err = conf.ValidateKeys()
	if err != nil {
		return err
	}

	// CPU性能分析
	if cpuProfile := c.String("cpuprofile"); cpuProfile != "" {
		if f, err := os.Create(cpuProfile); err != nil {
			return err
		} else {
			if err := pprof.StartCPUProfile(f); err != nil {
				f.Close()
				return err
			}
			// 确保在函数退出时停止CPU分析并关闭文件
			defer func() {
				pprof.StopCPUProfile()
				f.Close()
			}()
		}
	}

	// 内存性能分析
	if memProfile := c.String("memprofile"); memProfile != "" {
		if f, err := os.Create(memProfile); err != nil {
			return err
		} else {
			defer func() {
				// 在程序终止时运行内存分析
				runtime.GC()
				_ = pprof.WriteHeapProfile(f)
				_ = f.Close()
			}()
		}
	}

	// 创建本地节点对象
	currentNode, err := routing.NewLocalNode(conf)
	if err != nil {
		return err
	}

	// 初始化Prometheus监控
	if err := prometheus.Init(string(currentNode.NodeID()), currentNode.NodeType()); err != nil {
		return err
	}

	// 初始化服务器
	server, err := service.InitializeServer(conf, currentNode)
	if err != nil {
		return err
	}

	// 创建一个缓冲大小为1的信号通道，用于接收系统终止信号，os.Signal类型可以接收操作系统信号，缓冲大小为1意味着即使没有接收者，也能暂存1个信号，避免阻塞
	sigChan := make(chan os.Signal, 1)

	// 注册要接收的信号：signal.Notify将指定的信号转发到sigChan通道，监听的三个信号：
	// syscall.SIGINT(2)：中断信号，通常由Ctrl+C触发
	// syscall.SIGTERM(15)：终止信号，通常由kill命令发送
	// syscall.SIGQUIT(3)：退出信号，通常由Ctrl + \触发
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// 启动goroutine处理终止信号
	go func() {
		// 循环两次，第一次优雅停止，第二次强制停止
		for i := range 2 {
			sig := <-sigChan // 接收到第一/二个信号
			force := i > 0
			// 记录日志
			// 第一次信号: exit requested, shutting down {"signal": "xxx", "force": false}
            // 第二次信号: exit requested, shutting down {"signal": "xxx", "force": ture}
			logger.Infow("exit requested, shutting down", "signal", sig, "force", force)
			// 第一次force为false，优雅停止（等待现有连接处理完成），第二次force为true，强制停止（立即终止）
			go server.Stop(force)
		}
	}()

	// 启动服务器（阻塞直到服务器停止）
	return server.Start()
}

// getConfigString获取配置字符串，优先级：config-body > config-file
func getConfigString(configFile string, inConfigBody string) (string, error) {
	// 如果有直接传递的配置字符串，或者没有指定配置文件，直接返回
	if inConfigBody != "" || configFile == "" {
		return inConfigBody, nil
	}

	// 从配置文件读取
	outConfigBody, err := os.ReadFile(configFile)
	if err != nil {
		return "", err
	}

	return string(outConfigBody), nil
}


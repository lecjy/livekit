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

import (
	"context"
	"fmt"
	"os"
	"strconv"      // 字符串和基本数据类型之间的转换
	"strings"
	"time"

	// 第三方包：将数字转换为人类可读格式（如 1000 转为 "1KB"）
	"github.com/dustin/go-humanize"
	// 第三方包：在终端中创建格式化的表格
	"github.com/olekukonko/tablewriter"
	// 第三方包：用于构建命令行应用程序
	"github.com/urfave/cli/v3"
	// 第三方包：YAML 格式的编解码
	"gopkg.in/yaml.v3"

	// LiveKit 协议相关的内部包
	"github.com/livekit/protocol/auth"        // 认证和授权相关功能
	"github.com/livekit/protocol/livekit"      // LiveKit 协议定义和消息类型
	"github.com/livekit/protocol/utils"        // 通用工具函数
	"github.com/livekit/protocol/utils/guid"   // 生成全局唯一标识符

	// LiveKit 服务器内部包
	"github.com/livekit/livekit-server/pkg/config"   // 配置管理相关
	"github.com/livekit/livekit-server/pkg/routing"  // 节点路由功能
	"github.com/livekit/livekit-server/pkg/service"  // 核心服务实现
)

// generateKeys 生成并打印新的 API 密钥对
// 参数：context 上下文和 cli 命令对象（都被忽略）
// 返回：错误信息（总是 nil）
func generateKeys(_ context.Context, _ *cli.Command) error {
	// 生成新的 API Key，使用预定义的前缀
	apiKey := guid.New(utils.APIKeyPrefix)
	// 生成随机的 Secret
	secret := utils.RandomSecret()
	// 打印生成的密钥对
	fmt.Println("API Key: ", apiKey)
	fmt.Println("API Secret: ", secret)
	return nil
}

// printPorts 显示服务器使用的所有网络端口信息
// 参数：上下文（忽略）和 cli 命令对象
// 返回：可能的错误信息
func printPorts(_ context.Context, c *cli.Command) error {
	// 从命令行参数中加载配置
	conf, err := getConfig(c)
	if err != nil {
		return err
	}

	// 初始化 UDP 和 TCP 端口列表
	udpPorts := make([]string, 0)
	tcpPorts := make([]string, 0)

	// 添加 HTTP 服务端口
	tcpPorts = append(tcpPorts, fmt.Sprintf("%d - HTTP service", conf.Port))

	// 如果配置了 ICE/TCP 端口，添加到列表
	if conf.RTC.TCPPort != 0 {
		tcpPorts = append(tcpPorts, fmt.Sprintf("%d - ICE/TCP", conf.RTC.TCPPort))
	}

	// 处理 UDP 端口配置
	if conf.RTC.UDPPort.Valid() {
		// 如果是单个端口，获取端口字符串
		portStr, _ := conf.RTC.UDPPort.MarshalYAML()
		udpPorts = append(udpPorts, fmt.Sprintf("%s - ICE/UDP", portStr))
	} else {
		// 如果是端口范围，显示范围
		udpPorts = append(udpPorts, fmt.Sprintf("%d-%d - ICE/UDP range", conf.RTC.ICEPortRangeStart, conf.RTC.ICEPortRangeEnd))
	}

	// 如果启用了 TURN 服务器，添加 TURN 相关端口
	if conf.TURN.Enabled {
		// 添加 TURN/TLS 端口（TCP）
		if conf.TURN.TLSPort > 0 {
			tcpPorts = append(tcpPorts, fmt.Sprintf("%d - TURN/TLS", conf.TURN.TLSPort))
		}
		// 添加 TURN/UDP 端口
		if conf.TURN.UDPPort > 0 {
			udpPorts = append(udpPorts, fmt.Sprintf("%d - TURN/UDP", conf.TURN.UDPPort))
		}
	}

	// 打印 TCP 端口列表
	fmt.Println("TCP Ports")
	for _, p := range tcpPorts {
		fmt.Println(p)
	}

	// 打印 UDP 端口列表
	fmt.Println("UDP Ports")
	for _, p := range udpPorts {
		fmt.Println(p)
	}
	return nil
}

// helpVerbose 显示包含所有配置选项的详细帮助信息
// 参数：上下文（忽略）和 cli 命令对象
// 返回：可能的错误信息
func helpVerbose(_ context.Context, c *cli.Command) error {
	// 生成所有配置项对应的 CLI 标志（不包含隐藏标志）
	generatedFlags, err := config.GenerateCLIFlags(baseFlags, false)
	if err != nil {
		return err
	}

	// 合并基础标志和生成的标志
	flags := append([]cli.Flag{}, baseFlags...)
	flags = append(flags, generatedFlags...)
	
	// 设置根命令和当前命令的标志
	root := c.Root()
	root.Flags = flags
	c.Flags = flags
	
	// 显示应用程序的帮助信息
	return cli.ShowAppHelp(c)
}

// createToken 创建用于客户端认证的访问令牌
// 参数：上下文（忽略）和 cli 命令对象
// 返回：可能的错误信息
func createToken(_ context.Context, c *cli.Command) error {
	// 从命令行参数获取房间名称和参与者身份
	room := c.String("room")
	identity := c.String("identity")

	// 加载配置
	conf, err := getConfig(c)
	if err != nil {
		return err
	}

	// 从配置中获取 API 密钥
	if len(conf.Keys) == 0 {
		// 如果没有内联密钥，尝试从文件加载
		if _, err := os.Stat(conf.KeyFile); err != nil {
			return err
		}
		f, err := os.Open(conf.KeyFile)
		if err != nil {
			return err
		}
		// 确保文件在函数返回时关闭
		defer func() {
			_ = f.Close()
		}()
		// 解析 YAML 格式的密钥文件
		decoder := yaml.NewDecoder(f)
		if err = decoder.Decode(conf.Keys); err != nil {
			return err
		}

		// 验证是否成功加载了密钥
		if len(conf.Keys) == 0 {
			return fmt.Errorf("keys are not configured")
		}
	}

	// 使用配置中的第一个 API 密钥对
	var apiKey string
	var apiSecret string
	for k, v := range conf.Keys {
		apiKey = k
		apiSecret = v
		break
	}

	// 创建视频授权，允许加入指定房间
	grant := &auth.VideoGrant{
		RoomJoin: true,
		Room:     room,
	}
	
	// 如果是录音器模式，设置特殊权限
	if c.Bool("recorder") {
		grant.Hidden = true      // 隐藏参与者身份
		grant.Recorder = true    // 标记为录音器
		grant.SetCanPublish(false)      // 禁止发布音视频
		grant.SetCanPublishData(false)  // 禁止发布数据
	}

	// 创建访问令牌，设置身份和有效期（30天）
	at := auth.NewAccessToken(apiKey, apiSecret).
		AddGrant(grant).
		SetIdentity(identity).
		SetValidFor(30 * 24 * time.Hour)

	// 生成 JWT 格式的令牌
	token, err := at.ToJWT()
	if err != nil {
		return err
	}

	// 打印生成的令牌
	fmt.Println("Token:", token)

	return nil
}

// listNodes 显示所有 LiveKit 节点的状态信息
// 参数：上下文（忽略）和 cli 命令对象
// 返回：可能的错误信息
func listNodes(_ context.Context, c *cli.Command) error {
	// 加载配置
	conf, err := getConfig(c)
	if err != nil {
		return err
	}

	// 创建当前节点的本地表示
	currentNode, err := routing.NewLocalNode(conf)
	if err != nil {
		return err
	}

	// 初始化路由器
	router, err := service.InitializeRouter(conf, currentNode)
	if err != nil {
		return err
	}

	// 获取所有节点的列表
	nodes, err := router.ListNodes()
	if err != nil {
		return err
	}

	// 创建终端表格写入器
	table := tablewriter.NewWriter(os.Stdout)
	table.SetRowLine(true)           // 显示行分隔线
	table.SetAutoWrapText(false)      // 禁用自动换行
	
	// 设置表格列头，包含详细的节点统计信息
	table.SetHeader([]string{
		"ID", "IP Address", "Region",
		"CPUs", "CPU Usage\nLoad Avg",
		"Memory Used/Total",
		"Rooms", "Clients\nTracks In/Out",
		"Bytes/s In/Out\nBytes Total", "Packets/s In/Out\nPackets Total", "System Dropped Pkts/s\nPkts/s Out/Dropped",
		"Nack/s\nNack Total", "Retrans/s\nRetrans Total",
		"Started At\nUpdated At",
	})
	
	// 设置每列的对齐方式
	table.SetColumnAlignment([]int{
		tablewriter.ALIGN_CENTER, tablewriter.ALIGN_CENTER, tablewriter.ALIGN_CENTER,
		tablewriter.ALIGN_RIGHT, tablewriter.ALIGN_RIGHT,
		tablewriter.ALIGN_RIGHT,
		tablewriter.ALIGN_RIGHT, tablewriter.ALIGN_RIGHT,
		tablewriter.ALIGN_RIGHT, tablewriter.ALIGN_RIGHT, tablewriter.ALIGN_RIGHT,
		tablewriter.ALIGN_RIGHT, tablewriter.ALIGN_RIGHT,
		tablewriter.ALIGN_CENTER,
	})

	// 遍历每个节点，填充表格数据
	for _, node := range nodes {
		stats := node.Stats                    // 节点统计信息
		rate := &livekit.NodeStatsRate{}        // 节点速率信息
		if len(stats.Rates) > 0 {
			rate = stats.Rates[0]               // 使用最新的速率数据
		}

		// 格式化节点 ID 和状态
		idAndState := fmt.Sprintf("%s\n(%s)", node.Id, node.State.Enum().String())

		// 系统统计信息
		cpus := strconv.Itoa(int(stats.NumCpus))  // CPU 核心数
		// CPU 使用率和负载平均值（1分钟、5分钟、15分钟）
		cpuUsageAndLoadAvg := fmt.Sprintf("%.2f %%\n%.2f %.2f %.2f", stats.CpuLoad*100,
			stats.LoadAvgLast1Min, stats.LoadAvgLast5Min, stats.LoadAvgLast15Min)
		// 内存使用情况（已用/总量）
		memUsage := fmt.Sprintf("%s / %s", humanize.Bytes(stats.MemoryUsed), humanize.Bytes(stats.MemoryTotal))

		// 房间统计信息
		rooms := strconv.Itoa(int(stats.NumRooms))  // 房间数量
		// 客户端数量和音视频轨道数量（输入/输出）
		clientsAndTracks := fmt.Sprintf("%d\n%d / %d", stats.NumClients, stats.NumTracksIn, stats.NumTracksOut)

		// 数据包统计信息
		// 字节速率和总量
		bytes := fmt.Sprintf("%sps / %sps\n%s / %s", humanize.Bytes(uint64(rate.BytesIn)), humanize.Bytes(uint64(rate.BytesOut)),
			humanize.Bytes(stats.BytesIn), humanize.Bytes(stats.BytesOut))
		// 数据包速率和总量
		packets := fmt.Sprintf("%s / %s\n%s / %s", humanize.Comma(int64(rate.PacketsIn)), humanize.Comma(int64(rate.PacketsOut)),
			strings.TrimSpace(humanize.SIWithDigits(float64(stats.PacketsIn), 2, "")), strings.TrimSpace(humanize.SIWithDigits(float64(stats.PacketsOut), 2, "")))
		
		// 计算系统丢包率
		sysPacketsDroppedPct := float32(0)
		if rate.SysPacketsOut+rate.SysPacketsDropped > 0 {
			sysPacketsDroppedPct = float32(rate.SysPacketsDropped) / float32(rate.SysPacketsDropped+rate.SysPacketsOut)
		}
		sysPackets := fmt.Sprintf("%.2f %%\n%v / %v", sysPacketsDroppedPct*100, float64(rate.SysPacketsOut), float64(rate.SysPacketsDropped))
		
		// NACK（否定确认）统计
		nacks := fmt.Sprintf("%.2f\n%s", rate.NackTotal, strings.TrimSpace(humanize.SIWithDigits(float64(stats.NackTotal), 2, "")))
		// 重传统计
		retransmit := fmt.Sprintf("%.2f\n%s", rate.RetransmitPacketsOut, strings.TrimSpace(humanize.SIWithDigits(float64(stats.RetransmitPacketsOut), 2, "")))

		// 时间信息（启动时间和最后更新时间）
		startedAndUpdated := fmt.Sprintf("%s\n%s", time.Unix(stats.StartedAt, 0).UTC().UTC().Format("2006-01-02 15:04:05"),
			time.Unix(stats.UpdatedAt, 0).UTC().Format("2006-01-02 15:04:05"))

		// 将当前节点的数据添加到表格
		table.Append([]string{
			idAndState, node.Ip, node.Region,
			cpus, cpuUsageAndLoadAvg,
			memUsage,
			rooms, clientsAndTracks,
			bytes, packets, sysPackets,
			nacks, retransmit,
			startedAndUpdated,
		})
	}
	
	// 渲染并打印表格
	table.Render()

	return nil
}

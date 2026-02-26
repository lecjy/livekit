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

//go:build wireinject
// +build wireinject

package service

// 导入所需的包
import (
	"fmt"
	"os"

	"github.com/google/wire"           // Google的依赖注入工具，用于编译时生成依赖注入代码
	"github.com/pion/turn/v4"           // Pion TURN库，提供TURN服务器功能用于NAT穿透
	"github.com/pkg/errors"             // 增强的错误处理包，支持错误堆栈
	"github.com/redis/go-redis/v9"      // Redis客户端库，用于连接和操作Redis
	"gopkg.in/yaml.v3"                  // YAML解析库，用于解析配置文件

	// LiveKit协议相关包
	"github.com/livekit/protocol/auth"      // 认证相关，处理API密钥和令牌
	"github.com/livekit/protocol/livekit"    // LiveKit协议定义，包含所有protobuf消息类型
	"github.com/livekit/protocol/logger"     // 日志接口，提供结构化日志
	redisLiveKit "github.com/livekit/protocol/redis" // Redis工具包，封装了Redis客户端创建
	"github.com/livekit/protocol/rpc"        // RPC定义，包含所有服务间的RPC接口
	"github.com/livekit/protocol/utils"      // 工具函数，如版本生成器
	"github.com/livekit/protocol/webhook"    // Webhook处理，发送异步事件通知
	"github.com/livekit/psrpc"               // Pub/Sub RPC框架，用于节点间通信
	"github.com/livekit/psrpc/pkg/middleware/otelpsrpc" // OpenTelemetry中间件，用于分布式追踪

	// LiveKit服务器内部包
	"github.com/livekit/livekit-server/pkg/agent"      // Agent管理，处理AI Agent的生命周期
	"github.com/livekit/livekit-server/pkg/config"     // 配置管理，所有服务器配置定义
	"github.com/livekit/livekit-server/pkg/routing"    // 路由管理，节点间请求路由和负载均衡
	"github.com/livekit/livekit-server/pkg/sfu"        // SFU核心，选择性转发单元实现
	"github.com/livekit/livekit-server/pkg/telemetry"  // 遥测服务，收集和上报监控数据
)

// InitializeServer是整个LiveKit服务器的初始化函数，使用Wire进行依赖注入，该函数在编译时由Wire生成实际代码，用于组装所有服务组件
func InitializeServer(conf *config.Config, currentNode routing.LocalNode) (*LivekitServer, error) {
	// Wire.Build声明所有需要注入的依赖项，这是一个编译时指令，告诉Wire需要哪些构造函数来创建完整的服务器实例
	wire.Build(
		getNodeID,                    // 获取节点ID
		createRedisClient,            // 创建Redis客户端
		createStore,                  // 创建对象存储
		wire.Bind(new(ServiceStore), new(ObjectStore)), // 将ServiceStore接口绑定到ObjectStore实现
		createKeyProvider,             // 创建密钥提供者
		createWebhookNotifier,         // 创建Webhook通知器
		createForwardStats,            // 创建转发统计收集器
		getNodeStatsConfig,            // 获取节点统计配置
		routing.CreateRouter,          // 创建路由器
		getLimitConf,                  // 获取限流配置
		config.DefaultAPIConfig,       // 获取默认API配置
		wire.Bind(new(routing.MessageRouter), new(routing.Router)), // 绑定消息路由器接口
		wire.Bind(new(livekit.RoomService), new(*RoomService)),     // 绑定房间服务接口
		telemetry.NewAnalyticsService,  // 创建分析服务
		telemetry.NewTelemetryService,  // 创建遥测服务
		getMessageBus,                  // 获取消息总线
		NewIOInfoService,               // 创建IO信息服务
		wire.Bind(new(IOClient), new(*IOInfoService)), // 绑定IO客户端接口
		rpc.NewEgressClient,            // 创建Egress客户端
		rpc.NewIngressClient,           // 创建Ingress客户端
		getEgressStore,                 // 获取Egress存储接口
		NewEgressLauncher,              // 创建Egress启动器
		NewEgressService,               // 创建Egress服务
		getIngressStore,                // 获取Ingress存储接口
		getIngressConfig,               // 获取Ingress配置
		NewIngressService,              // 创建Ingress服务
		rpc.NewSIPClientWithParams,     // 创建SIP客户端
		getSIPStore,                    // 获取SIP存储接口
		getSIPConfig,                   // 获取SIP配置
		NewSIPService,                  // 创建SIP服务
		NewRoomAllocator,               // 创建房间分配器
		NewRoomService,                 // 创建房间服务
		NewRTCService,                  // 创建RTC服务
		NewWHIPService,                  // 创建WHIP服务
		NewAgentService,                 // 创建Agent服务
		NewAgentDispatchService,         // 创建Agent调度服务
		getAgentConfig,                  // 获取Agent配置
		agent.NewAgentClient,            // 创建Agent客户端
		getAgentStore,                   // 获取Agent存储接口
		getSignalRelayConfig,            // 获取信令中继配置
		NewDefaultSignalServer,          // 创建默认信令服务器
		routing.NewSignalClient,         // 创建信令客户端
		getRoomConfig,                   // 获取房间配置
		routing.NewRoomManagerClient,    // 创建房间管理器客户端
		rpc.NewKeepalivePubSub,          // 创建保活PubSub
		getPSRPCConfig,                  // 获取PSRPC配置
		getPSRPCClientParams,            // 获取PSRPC客户端参数
		rpc.NewTopicFormatter,           // 创建主题格式化器
		rpc.NewTypedRoomClient,          // 创建类型化的房间客户端
		rpc.NewTypedParticipantClient,   // 创建类型化的参与者客户端
		rpc.NewTypedWHIPParticipantClient, // 创建类型化的WHIP参与者客户端
		rpc.NewTypedAgentDispatchInternalClient, // 创建类型化的Agent调度内部客户端
		NewLocalRoomManager,              // 创建本地房间管理器
		NewTURNAuthHandler,               // 创建TURN认证处理器
		getTURNAuthHandlerFunc,           // 获取TURN认证处理器函数
		newInProcessTurnServer,           // 创建进程内TURN服务器
		utils.NewDefaultTimedVersionGenerator, // 创建版本生成器
		NewLivekitServer,                  // 创建LiveKit服务器实例
	)
	// 注意：这个函数不会被实际调用，Wire会生成一个同名函数来替换它
	// 这里返回空值是因为Wire会在编译时生成真正的实现
	return &LivekitServer{}, nil
}

// InitializeRouter 是一个简化的初始化函数，只创建路由器组件
// 用于只需要路由功能的场景（如测试或单独的路由服务）
func InitializeRouter(conf *config.Config, currentNode routing.LocalNode) (routing.Router, error) {
	// 声明创建路由器所需的依赖项
	wire.Build(
		createRedisClient,           // 创建Redis客户端
		getNodeID,                   // 获取节点ID
		getMessageBus,               // 获取消息总线
		getSignalRelayConfig,        // 获取信令中继配置
		getPSRPCConfig,              // 获取PSRPC配置
		getPSRPCClientParams,        // 获取PSRPC客户端参数
		routing.NewSignalClient,     // 创建信令客户端
		getRoomConfig,               // 获取房间配置
		routing.NewRoomManagerClient, // 创建房间管理器客户端
		rpc.NewKeepalivePubSub,      // 创建保活PubSub
		getNodeStatsConfig,          // 获取节点统计配置
		routing.CreateRouter,        // 创建路由器
	)

	return nil, nil
}

// getNodeID 从当前节点信息中提取节点ID
// 用于唯一标识集群中的这个节点
func getNodeID(currentNode routing.LocalNode) livekit.NodeID {
	return currentNode.NodeID()
}

// createKeyProvider 根据配置创建密钥提供者
// 支持从文件加载密钥或直接从配置中读取
// 如果指定了密钥文件，会检查文件权限（必须不允许其他用户访问）
func createKeyProvider(conf *config.Config) (auth.KeyProvider, error) {
	// prefer keyfile if set
	// 如果配置了密钥文件，优先从文件加载
	if conf.KeyFile != "" {
		// 检查文件权限，确保其他用户没有读写执行权限（安全性检查）
		var otherFilter os.FileMode = 0007
		if st, err := os.Stat(conf.KeyFile); err != nil {
			return nil, err
		} else if st.Mode().Perm()&otherFilter != 0000 {
			return nil, fmt.Errorf("key file others permissions must be set to 0")
		}
		// 打开密钥文件
		f, err := os.Open(conf.KeyFile)
		if err != nil {
			return nil, err
		}
		defer func() {
			_ = f.Close()
		}()
		// 解析YAML格式的密钥文件
		decoder := yaml.NewDecoder(f)
		if err = decoder.Decode(conf.Keys); err != nil {
			return nil, err
		}
	}

	// 确保至少有一个密钥可用，否则无法进行安全认证
	if len(conf.Keys) == 0 {
		return nil, errors.New("one of key-file or keys must be provided in order to support a secure installation")
	}

	// 从密钥映射创建文件型密钥提供者
	return auth.NewFileBasedKeyProviderFromMap(conf.Keys), nil
}

// createWebhookNotifier 创建Webhook通知器
// 用于向配置的URL发送异步的Webhook事件（如房间创建、参与者加入等）
func createWebhookNotifier(conf *config.Config, provider auth.KeyProvider) (webhook.QueuedNotifier, error) {
	wc := conf.WebHook

	// 获取指定API密钥对应的密钥，用于签名Webhook请求
	secret := provider.GetSecret(wc.APIKey)
	// 如果配置了Webhook URL但没有提供API密钥，返回错误
	if secret == "" && len(wc.URLs) > 0 {
		return nil, ErrWebHookMissingAPIKey
	}

	// 创建默认的Webhook通知器，支持队列和重试
	return webhook.NewDefaultNotifier(wc, provider)
}

// createRedisClient 根据配置创建Redis客户端
// 如果Redis未配置，返回nil，表示使用本地模式
func createRedisClient(conf *config.Config) (redis.UniversalClient, error) {
	if !conf.Redis.IsConfigured() {
		return nil, nil
	}
	// 使用协议包中的工具创建Redis客户端（支持单机、集群、哨兵模式）
	return redisLiveKit.GetRedisClient(&conf.Redis)
}

// createStore 根据是否配置了Redis创建相应的存储实现
// Redis配置了则使用Redis存储（分布式），否则使用本地内存存储（单机）
func createStore(rc redis.UniversalClient) ObjectStore {
	if rc != nil {
		return NewRedisStore(rc)  // Redis存储，支持分布式部署
	}
	return NewLocalStore()        // 本地内存存储，仅用于单机模式
}

// getMessageBus 根据Redis客户端创建消息总线
// 有Redis则创建Redis消息总线（分布式），否则创建本地消息总线（单机）
func getMessageBus(rc redis.UniversalClient) psrpc.MessageBus {
	if rc == nil {
		return psrpc.NewLocalMessageBus()  // 本地内存消息总线
	}
	return psrpc.NewRedisMessageBus(rc)    // 基于Redis Pub/Sub的消息总线
}

// getEgressStore 从对象存储中获取Egress存储接口
// 只有Redis存储支持Egress功能，因为Egress需要持久化和分布式访问
func getEgressStore(s ObjectStore) EgressStore {
	switch store := s.(type) {
	case *RedisStore:
		return store  // Redis存储支持Egress
	default:
		return nil    // 本地存储不支持Egress
	}
}

// getIngressStore 从对象存储中获取Ingress存储接口
// 只有Redis存储支持Ingress功能，因为Ingress需要持久化和分布式访问
func getIngressStore(s ObjectStore) IngressStore {
	switch store := s.(type) {
	case *RedisStore:
		return store  // Redis存储支持Ingress
	default:
		return nil    // 本地存储不支持Ingress
	}
}

// getAgentStore 从对象存储中获取Agent存储接口
// Redis存储和本地存储都支持Agent功能，因为Agent可以在单机运行
func getAgentStore(s ObjectStore) AgentStore {
	switch store := s.(type) {
	case *RedisStore:
		return store  // Redis存储支持Agent
	case *LocalStore:
		return store  // 本地存储也支持Agent
	default:
		return nil
	}
}

// getIngressConfig 从主配置中提取Ingress子配置
// 包含RTMP端口、最大带宽、支持的协议等
func getIngressConfig(conf *config.Config) *config.IngressConfig {
	return &conf.Ingress
}

// getSIPStore 从对象存储中获取SIP存储接口
// 只有Redis存储支持SIP功能，因为SIP通话需要跨节点协调
func getSIPStore(s ObjectStore) SIPStore {
	switch store := s.(type) {
	case *RedisStore:
		return store  // Redis存储支持SIP
	default:
		return nil    // 本地存储不支持SIP
	}
}

// getSIPConfig 从主配置中提取SIP子配置
// 包含SIP域名、端口、中继提供商配置等
func getSIPConfig(conf *config.Config) *config.SIPConfig {
	return &conf.SIP
}

// getLimitConf 从主配置中提取限流配置
// 用于控制资源使用，如最大参与者数、最大轨道数等
func getLimitConf(config *config.Config) config.LimitConfig {
	return config.Limit
}

// getRoomConfig 从主配置中提取房间配置
// 包含空置超时、自动创建、默认房间设置等
func getRoomConfig(config *config.Config) config.RoomConfig {
	return config.Room
}

// getSignalRelayConfig 从主配置中提取信令中继配置
// 用于配置节点间信令转发的超时和重试
func getSignalRelayConfig(config *config.Config) config.SignalRelayConfig {
	return config.SignalRelay
}

// getPSRPCConfig 从主配置中提取PSRPC配置
// 包含客户端池大小、请求超时、消息大小限制等
func getPSRPCConfig(config *config.Config) rpc.PSRPCConfig {
	return config.PSRPC
}

// getPSRPCClientParams 创建PSRPC客户端参数
// 配置日志、监控和OpenTelemetry中间件，用于分布式追踪
func getPSRPCClientParams(config rpc.PSRPCConfig, bus psrpc.MessageBus) rpc.ClientParams {
	return rpc.NewClientParams(config, bus, logger.GetLogger(), rpc.PSRPCMetricsObserver{},
		otelpsrpc.ClientOptions(otelpsrpc.Config{}), // 添加OpenTelemetry支持
	)
}

// createForwardStats 创建转发统计收集器
// 如果配置了统计间隔等参数则创建，否则返回nil
// 用于监控SFU的媒体转发质量
func createForwardStats(conf *config.Config) *sfu.ForwardStats {
	// 检查是否配置了统计参数
	if conf.RTC.ForwardStats.SummaryInterval == 0 || conf.RTC.ForwardStats.ReportInterval == 0 || conf.RTC.ForwardStats.ReportWindow == 0 {
		return nil
	}
	// 创建转发统计收集器
	return sfu.NewForwardStats(conf.RTC.ForwardStats.SummaryInterval, conf.RTC.ForwardStats.ReportInterval, conf.RTC.ForwardStats.ReportWindow)
}

// newInProcessTurnServer 创建进程内的TURN服务器
// 用于NAT穿透，帮助客户端建立直接连接
func newInProcessTurnServer(conf *config.Config, authHandler turn.AuthHandler) (*turn.Server, error) {
	return NewTurnServer(conf, authHandler, false)
}

// getNodeStatsConfig 从主配置中提取节点统计配置
// 包含统计上报间隔、告警阈值等
func getNodeStatsConfig(config *config.Config) config.NodeStatsConfig {
	return config.NodeStats
}

// getAgentConfig 从主配置中提取Agent配置
// 包含空闲超时、最大并发数、健康检查间隔等
func getAgentConfig(config *config.Config) agent.Config {
	return config.Agents
}

# 火山方舟 v3 单向流式语音设计

## 目标

在火山方舟渠道类型（`45`）中补齐豆包语音 v3 支持，首期覆盖：

- WebSocket 单向流式：`wss://openspeech.bytedance.com/api/v3/tts/unidirectional/stream`
- HTTP Chunked：`https://openspeech.bytedance.com/api/v3/tts/unidirectional`
- 新控制台 API Key 鉴权
- 旧控制台 `appid|access_token` 鉴权

现有 v1 `ws_binary` 行为必须保持兼容，未配置 v3 的渠道不得改变现有行为。

## 非目标

- WebSocket 双向流式
- HTTP SSE
- 改造通用渠道测试按钮
- 将豆包语音放入 `DoubaoVideo` 渠道类型
- 允许客户端通过 `metadata` 切换渠道协议、鉴权方式或计费资源

## 方案选择

基于上游 PR `QuantumNous/new-api#4710` 已验证的 v3 协议帧实现进行裁剪，只保留 WebSocket 单向流式和 HTTP Chunked，并修复审查中发现的连接超时、请求取消和配置默认值问题。

不直接合入完整 PR，避免引入当前不需要的双向 WebSocket、HTTP SSE、旧前端目录修改以及尚未处理的审查问题。也不从零重新实现火山二进制事件协议，避免重复承担帧格式和事件顺序风险。

## 渠道配置

在现有 `channels.settings` JSON 中增加 `volc_tts`，不增加数据库字段，保持 SQLite、MySQL 和 PostgreSQL 兼容。

```json
{
  "volc_tts": {
    "protocol": "v3_ws_uni",
    "resource_id": "seed-tts-2.0",
    "auth_mode": "new_console",
    "require_usage": true
  }
}
```

字段定义：

- `protocol`
  - `v1_ws_binary`
  - `v3_ws_uni`
  - `v3_http_chunked`
- `resource_id`：火山 `X-Api-Resource-Id`，v3 模式必填
- `auth_mode`
  - `new_console`：渠道密钥为单个 API Key，发送 `X-Api-Key`
  - `legacy`：渠道密钥必须为 `appid|access_token`，发送 `X-Api-App-Id` 和 `X-Api-Access-Key`
- `require_usage`：默认开启，请求火山返回实际用量

未配置 `volc_tts` 或协议为空时继续使用 v1 `ws_binary`。

## 模型与资源

火山方舟内置模型列表增加：

- `seed-tts-1.0-concurr`
- `seed-tts-2.0`
- `seed-icl-2.0`

模型名称用于 new-api 路由；`resource_id` 用于选择火山实际语音资源和计费层级。建议一个渠道只承载一种资源家族，并让模型名称与 `resource_id` 保持一致。需要多个资源家族时创建多个火山方舟渠道。

不允许普通客户端通过请求 `metadata` 覆盖协议、鉴权方式或 `resource_id`，防止绕过渠道资源边界和计费配置。

`voice` 直接接受火山官方音色 ID。现有 OpenAI 音色别名映射保留给旧版渠道；v3 不自动将旧版音色映射到 Seed-TTS 2.0，避免资源 ID 与音色家族不匹配。

## 请求转换

客户端继续调用 OpenAI 风格接口：

```http
POST /v1/audio/speech
```

```json
{
  "model": "seed-tts-2.0",
  "input": "你好，这是语音测试",
  "voice": "火山官方音色 ID",
  "response_format": "mp3",
  "speed": 1
}
```

适配器将 `input`、`voice`、输出格式、采样率和语速转换为火山 v3 `StartSession` 请求。WebSocket 单向流式和 HTTP Chunked 都解析火山二进制事件帧，只将音频负载写回客户端。

## WebSocket 单向流式

处理流程：

1. 建立带 v3 鉴权头的 WebSocket 连接。
2. 发送 `StartConnection`。
3. 发送包含完整文本的 `StartSession`。
4. 持续读取音频事件并写入 HTTP 响应。
5. 解析 `SessionFinished` 的 usage。
6. 结束会话和连接。

WebSocket 读取必须设置滑动空闲超时，并监听客户端请求上下文。客户端断开或请求取消时关闭上游连接，使阻塞读取及时退出。

## HTTP Chunked

HTTP Chunked 使用专用 `http.Client`：

- 设置连接超时
- 设置 TLS 握手超时
- 设置响应头超时
- 不设置整个请求的 `Client.Timeout`，避免长语音流被中断

响应体按火山 v3 二进制帧边界解析。音频事件写回客户端，完成事件用于提取 usage，错误事件转换为统一上游错误。

## 用量与计费

优先读取火山 `SessionFinished` 中的 `usage.text_words`，作为音频请求的实际文本用量。上游未返回 usage 时，回退到当前字符数估算逻辑。

日志中记录协议和 `resource_id`，便于定位渠道配置及计费问题。火山返回的 `X-Tt-Logid` 同时写入响应头和诊断信息。

## 错误处理

- v3 模式缺少 `resource_id`：请求前返回 400
- `legacy` 密钥格式错误：请求前返回渠道密钥错误
- WebSocket 握手失败：保留状态及火山错误信息
- HTTP 非 200：限制读取错误响应体，避免无界内存使用
- 火山错误事件：转换为统一 `NewAPIError`
- 无效或截断协议帧：返回协议解析错误
- 客户端取消：停止上游读取，不记录为普通服务故障

## 管理界面

仅在火山方舟类型中显示“豆包语音配置”：

- 协议选择
- 资源 ID 常用选项及自定义输入
- 鉴权模式选择
- 返回 usage 开关

v3 模式下资源 ID 必填。界面解释模型名称与资源 ID 的区别，并根据鉴权模式提示正确密钥格式。所有新增文字使用项目 i18n。

通用渠道测试按钮首期不改造，因为它当前测试聊天接口；语音渠道通过 `/v1/audio/speech` 验证。

## 测试策略

后端测试使用 `testify/require` 和 `testify/assert`，覆盖：

- 渠道配置解析和 v1 回退
- v3 模式资源 ID 校验
- 新旧控制台鉴权头
- OpenAI 请求到 v3 请求的字段转换
- WebSocket 单向流式事件顺序、音频输出、usage、取消和超时
- HTTP Chunked 拆帧、音频输出、usage 和上游错误
- 模型列表新增项
- v1 `ws_binary` 回归

前端验证包括 TypeScript 检查、构建和 i18n 同步检查。最终运行目标包测试、相关服务测试、`go test ./...` 和 `go build ./...`。

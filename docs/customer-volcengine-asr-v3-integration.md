# 豆包录音文件识别 ASR v3 接入文档

## 1. 接口说明

本服务兼容火山引擎豆包录音文件识别 ASR v3 协议。客户端通过 New API 网关提交音频识别任务并查询结果，无需持有或传递火山引擎 API Key。

接口采用异步模式：

1. 调用提交接口创建识别任务。
2. 保存响应头中的 `X-Api-Request-Id`。
3. 使用该请求 ID 调用查询接口获取识别结果。

接口地址中的域名由服务提供方分配。本文使用以下占位地址：

```text
https://your-new-api-domain.example.com
```

## 2. 鉴权方式

所有请求使用 New API Key 进行 Bearer 鉴权：

```http
Authorization: Bearer <NEW_API_KEY>
```

还需指定 ASR 资源 ID：

```http
X-Api-Resource-Id: volc.seedasr.auc
```

客户端不得传递火山引擎 API Key。网关会使用服务端配置的火山凭证完成上游鉴权。

## 3. 提交识别任务

### 请求

```text
POST /api/v3/auc/bigmodel/submit
```

请求头：

```http
Authorization: Bearer <NEW_API_KEY>
Content-Type: application/json
X-Api-Resource-Id: volc.seedasr.auc
```

可以自行生成 UUID 并通过 `X-Api-Request-Id` 传入。未提供时，网关会自动生成，并在响应头中返回。

请求体：

```json
{
  "user": {
    "uid": "customer-user-id"
  },
  "audio": {
    "url": "https://example.com/audio.mp3",
    "format": "mp3",
    "codec": "raw",
    "rate": 16000,
    "bits": 16,
    "channel": 1
  },
  "request": {
    "model_name": "bigmodel",
    "enable_itn": true,
    "enable_punc": true,
    "enable_ddc": false,
    "enable_speaker_info": false,
    "enable_channel_split": false,
    "show_utterances": true,
    "vad_segment": false,
    "sensitive_words_filter": ""
  }
}
```

主要参数：

| 字段 | 必填 | 说明 |
| --- | --- | --- |
| `user.uid` | 是 | 客户侧用户标识，不应包含敏感信息 |
| `audio.url` | 是 | 可由火山服务器公网访问的音频 URL |
| `audio.format` | 建议 | 音频格式，例如 `mp3`、`wav` |
| `audio.rate` | 建议 | 采样率，例如 `16000` |
| `request.model_name` | 是 | 使用 `bigmodel` |
| `request.enable_itn` | 否 | 是否执行数字、日期等文本规范化 |
| `request.enable_punc` | 否 | 是否添加标点符号 |
| `request.show_utterances` | 否 | 设为 `true` 时返回分句及字级时间戳 |

### cURL 示例

```bash
curl -X POST 'https://your-new-api-domain.example.com/api/v3/auc/bigmodel/submit' \
  -H 'Authorization: Bearer <NEW_API_KEY>' \
  -H 'Content-Type: application/json' \
  -H 'X-Api-Resource-Id: volc.seedasr.auc' \
  -d '{
    "user": {
      "uid": "customer-user-id"
    },
    "audio": {
      "url": "https://example.com/audio.mp3",
      "format": "mp3",
      "codec": "raw",
      "rate": 16000,
      "bits": 16,
      "channel": 1
    },
    "request": {
      "model_name": "bigmodel",
      "enable_itn": true,
      "enable_punc": true,
      "show_utterances": true
    }
  }'
```

### 成功响应

提交成功时 HTTP 状态为 `200`，关键响应头如下：

```http
X-Api-Status-Code: 20000000
X-Api-Message: OK
X-Api-Request-Id: 6aa1c499-e9eb-42da-9615-f03f3bf7697b
```

响应体通常为：

```json
{}
```

必须保存 `X-Api-Request-Id`，后续查询需要使用该值。

## 4. 查询识别结果

### 请求

```text
POST /api/v3/auc/bigmodel/query
```

请求头：

```http
Authorization: Bearer <NEW_API_KEY>
Content-Type: application/json
X-Api-Resource-Id: volc.seedasr.auc
X-Api-Request-Id: <提交接口返回的请求ID>
```

请求体：

```json
{}
```

### cURL 示例

```bash
curl -X POST 'https://your-new-api-domain.example.com/api/v3/auc/bigmodel/query' \
  -H 'Authorization: Bearer <NEW_API_KEY>' \
  -H 'Content-Type: application/json' \
  -H 'X-Api-Resource-Id: volc.seedasr.auc' \
  -H 'X-Api-Request-Id: <提交接口返回的请求ID>' \
  -d '{}'
```

任务尚未完成时，客户端可间隔 1 至 2 秒再次查询。请避免无间隔高频轮询。

### 成功响应示例

```json
{
  "audio_info": {
    "duration": 6312
  },
  "result": {
    "additions": {
      "duration": "6312"
    },
    "text": "识别得到的完整文本。",
    "utterances": [
      {
        "start_time": 480,
        "end_time": 5880,
        "text": "识别得到的完整文本。",
        "words": [
          {
            "text": "识",
            "start_time": 480,
            "end_time": 600,
            "confidence": 0
          }
        ]
      }
    ]
  }
}
```

时间字段单位为毫秒：

| 字段 | 说明 |
| --- | --- |
| `audio_info.duration` | 音频总时长 |
| `utterances[].start_time` | 分句开始时间 |
| `utterances[].end_time` | 分句结束时间 |
| `utterances[].words[].start_time` | 单字或词开始时间 |
| `utterances[].words[].end_time` | 单字或词结束时间 |

只有提交任务时设置 `show_utterances: true`，查询结果才会包含 `utterances` 和 `words`。

## 5. JavaScript 示例

```javascript
const baseURL = 'https://your-new-api-domain.example.com'
const apiKey = process.env.NEW_API_KEY
const resourceId = 'volc.seedasr.auc'

async function submitASR(audioUrl) {
  const response = await fetch(`${baseURL}/api/v3/auc/bigmodel/submit`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${apiKey}`,
      'Content-Type': 'application/json',
      'X-Api-Resource-Id': resourceId,
    },
    body: JSON.stringify({
      user: { uid: 'customer-user-id' },
      audio: {
        url: audioUrl,
        format: 'mp3',
        codec: 'raw',
        rate: 16000,
        bits: 16,
        channel: 1,
      },
      request: {
        model_name: 'bigmodel',
        enable_itn: true,
        enable_punc: true,
        show_utterances: true,
      },
    }),
  })

  if (!response.ok) {
    throw new Error(`ASR submit failed: ${response.status} ${await response.text()}`)
  }

  const requestId = response.headers.get('X-Api-Request-Id')
  if (!requestId) {
    throw new Error('ASR submit response does not contain X-Api-Request-Id')
  }
  return requestId
}

async function queryASR(requestId) {
  const response = await fetch(`${baseURL}/api/v3/auc/bigmodel/query`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${apiKey}`,
      'Content-Type': 'application/json',
      'X-Api-Resource-Id': resourceId,
      'X-Api-Request-Id': requestId,
    },
    body: '{}',
  })

  const body = await response.text()
  if (!response.ok) {
    throw new Error(`ASR query failed: ${response.status} ${body}`)
  }
  return body ? JSON.parse(body) : null
}
```

在浏览器环境调用时，服务端必须允许前端读取 `X-Api-Request-Id` 响应头。生产环境更推荐由客户后端调用，避免将 New API Key 暴露在网页中。

## 6. 错误处理

客户端应同时检查 HTTP 状态码、`X-Api-Status-Code` 和响应体。

| 情况 | 说明 |
| --- | --- |
| HTTP `401` | New API Key 缺失或无效 |
| HTTP `403` | 权限不足，或上游资源未授权 |
| HTTP `503` | 当前分组没有支持该模型的可用渠道 |
| `X-Api-Status-Code: 20000000` | 火山接口处理成功 |
| `requested resource not granted` | 火山 API Key 未获得对应 Resource ID 权限 |

发生错误时，请记录以下信息并提供给服务方排查：

- HTTP 状态码
- `X-Api-Status-Code`
- `X-Api-Message`
- `X-Api-Request-Id`
- `X-Tt-Logid`
- 响应体

不要在日志或工单中提交完整的 `Authorization` 密钥。

## 7. 接入注意事项

1. 音频 URL 必须能够被公网访问，且不能依赖登录态或临时浏览器 Cookie。
2. 提交和查询必须使用相同的 `X-Api-Resource-Id`。
3. 查询必须使用提交接口返回的 `X-Api-Request-Id`。
4. 需要字级时间戳时，提交请求必须设置 `show_utterances: true`。
5. 客户端应设置合理的连接、读取和总请求超时时间。
6. 不要在客户端代码、日志或截图中公开 New API Key。

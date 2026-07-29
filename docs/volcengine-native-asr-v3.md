# 火山方舟原生 ASR v3

火山方舟（VolcEngine）渠道支持透传豆包录音文件识别 v3 协议，保留火山原生响应字段，包括 `utterances[].words[]` 的字级时间戳。

## 渠道配置

在渠道的其他设置中启用 ASR：

- 协议：`v3_auc`
- Resource ID：`volc.bigasr.auc`（1.0）或 `volc.seedasr.auc`（2.0）
- 鉴权模式：新控制台使用 `new_console`，旧版控制台使用 `legacy`

新控制台 API Key 直接填写 API Key。旧版控制台 API Key 使用 `AppKey|AccessKey` 格式。ASR 配置应放在火山方舟类型渠道中，不放在 `doubaovideo` 类型。

## 提交任务

请求地址：

```text
POST http://localhost:3001/api/v3/auc/bigmodel/submit
```

请求头至少包含：

```http
Authorization: Bearer <new-api-token>
X-Api-Resource-Id: volc.seedasr.auc
Content-Type: application/json
```

网关会使用渠道配置中的凭据向火山官方地址发送请求，并自动补充 `X-Api-Request-Id` 和 `X-Api-Sequence: -1`。

请求体使用火山原生格式：

```json
{
  "user": {"uid": "new-api-user"},
  "audio": {"url": "https://example.com/audio.wav"},
  "request": {
    "model_name": "bigmodel",
    "show_utterances": true,
    "enable_itn": true,
    "enable_punc": true
  }
}
```

提交响应原样返回火山的 JSON，例如其中的任务标识和状态字段。

## 查询任务

请求地址：

```text
POST http://localhost:3001/api/v3/auc/bigmodel/query
```

使用与提交相同的 `X-Api-Request-Id`，并发送火山原生查询请求体（没有业务字段时发送 `{}`）：

```json
{}
```

查询响应原样透传。启用 `show_utterances` 且任务完成后，响应中的字级时间戳位于：

```text
utterances[].words[].text
utterances[].words[].start_time
utterances[].words[].end_time
utterances[].words[].blank_duration
```

网关不会将原生响应改写为 OpenAI 格式，也不会丢弃这些字段。

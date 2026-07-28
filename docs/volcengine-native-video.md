# 火山方舟原生 Seedance 视频

火山方舟类型渠道支持两种 Seedance 视频提交方式：保持 OpenAI 兼容的 `/v1/videos`，以及火山方舟原生的任务创建地址。客户端只传 New API 的令牌；网关会使用所选火山方舟渠道中配置的上游凭据向火山发送请求。

## 渠道配置

将 Seedance 模型配置在火山方舟类型渠道中，并为渠道配置对应的火山方舟 API Key 和 Base URL。客户端请求使用 New API 令牌，不需要也不应传递火山方舟 API Key。

```http
Authorization: Bearer <new-api-token>
Content-Type: application/json
```

## OpenAI 兼容接口

请求地址：

```text
POST http://<new-api-host>/v1/videos
```

当 `model` 为 Seedance/豆包视频模型时，网关会把 OpenAI 兼容的 `width` 和 `height` 转换为火山方舟的 `resolution` 和 `ratio`。例如，`720x1280` 会转换为 `720p` 和 `9:16`：

```json
{
  "model": "doubao-seedance-2-0-260128",
  "prompt": "一个连衣裙从黑色逐渐变成白色",
  "duration": 15,
  "width": 720,
  "height": 1280,
  "seed": 646957806
}
```

提交到火山方舟的等价核心参数为：

```json
{
  "model": "doubao-seedance-2-0-260128",
  "content": [
    {
      "type": "text",
      "text": "一个连衣裙从黑色逐渐变成白色"
    }
  ],
  "duration": 15,
  "resolution": "720p",
  "ratio": "9:16",
  "seed": 646957806
}
```

已支持的尺寸映射为：

| `width` x `height` | `resolution` | `ratio` |
| --- | --- | --- |
| `1920x1080` | `1080p` | `16:9` |
| `1080x1920` | `1080p` | `9:16` |
| `1280x720` | `720p` | `16:9` |
| `720x1280` | `720p` | `9:16` |

直接传入的 `resolution` 和 `ratio` 优先于由 `width` 和 `height` 推导出的值。`width` 和 `height` 必须同时提供；不在上表中的组合会返回 `400 invalid_request`，不会把无效尺寸传给上游。

除网关保留字段外，未知的顶层兼容参数会继续转发给火山方舟，而不是被静默丢弃。例如 `fps`、`n`、`response_format` 和 `user` 会随上游请求转发。火山方舟是否接受这些字段由具体模型和官方协议决定；上游拒绝时会返回其错误。`model`、`content` 和 `duration` 由网关根据兼容请求规范组装，额外字段不能覆盖它们。视频时长必须为正数且不得超过网关的任务时长上限。

该接口的成功响应仍是 OpenAI 兼容的视频任务对象，任务 ID 为 New API 的公开任务 ID。

## 火山方舟原生接口

请求地址：

```text
POST http://<new-api-host>/api/v3/contents/generations/tasks
```

请求体直接使用火山方舟 Seedance 的任务创建格式。示例：

```bash
curl -X POST 'http://<new-api-host>/api/v3/contents/generations/tasks' \
  -H 'Authorization: Bearer <new-api-token>' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "doubao-seedance-2-0-260128",
    "content": [
      {
        "type": "text",
        "text": "一个连衣裙从黑色逐渐变成白色"
      }
    ],
    "duration": 15,
    "resolution": "720p",
    "ratio": "9:16",
    "seed": 646957806
  }'
```

成功时，网关原样返回火山方舟的创建响应，例如：

```json
{
  "id": "cgt-20260728123456-example"
}
```

原生接口不把响应转换为 OpenAI 视频任务对象。火山方舟返回的非 200 状态码、响应头中的 `Content-Type` 和错误 JSON 也会原样透传；例如模型不支持某个 `duration` 时，客户端会直接收到火山方舟的 `InvalidParameter` 错误及其原始错误内容。

创建接口返回的是 New API 的 `task_...` 任务 ID。火山的 `cgt-...` 上游 ID 仅在网关内部保存，用于转发查询请求。使用返回的 `task_...` 查询状态：

```text
GET http://<new-api-host>/api/v3/contents/generations/tasks/<task-id>
```

该查询同样使用 New API 的 Bearer 令牌，并将火山方舟任务状态原样返回。

原生请求要求 `model` 和至少一个非空文本 `content` 项。`duration` 若显式提供，不能为 `0`，也不能超过网关的任务时长上限。其余原生字段按请求内容转发，由火山方舟根据模型能力进行校验。

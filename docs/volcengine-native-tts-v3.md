# Native Volcengine TTS v3

New API exposes the Volcengine native HTTP Chunked protocol at:

```text
POST /api/v3/tts/unidirectional
```

The client authenticates with a New API token in `X-Api-Key`. The gateway replaces that token with the Volcengine credential configured on the selected Volcengine channel.

Required headers:

```text
X-Api-Key: sk-newapi-token
X-Api-Resource-Id: seed-tts-2.0
Content-Type: application/json
```

Optional UUID correlation headers are forwarded when valid:

```text
X-Api-Request-Id: 11111111-1111-4111-8111-111111111111
X-Api-Connect-Id: 22222222-2222-4222-8222-222222222222
```

Example request:

```json
{
  "namespace": "UnidirectionalTTS",
  "req_params": {
    "text": "你好，世界",
    "speaker": "seed-voice",
    "audio_params": {
      "format": "mp3",
      "enable_subtitle": true
    }
  }
}
```

The response is newline-delimited JSON. Each complete upstream record is forwarded in order, including base64 `data`, `sentence.words`, `sentence.phonemes`, and the final `usage` record. Enabling `audio_params.enable_subtitle` requests word-level timestamps from Volcengine.

Configure the selected channel with the Volcengine TTS v3 HTTP Chunked protocol, the matching `resource_id` (for example `seed-tts-2.0`), and either new-console or legacy authentication. The OpenAI-compatible `/v1/audio/speech` endpoint remains unchanged.

/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import test from 'node:test'

import { CHANNEL_FORM_DEFAULT_VALUES, channelFormSchema } from '../channel-form'
import {
  parseVolcTTSSettings,
  serializeVolcTTSSettings,
} from '../volcengine-tts'

test('parses volc_tts settings into form defaults', () => {
  const defaults = parseVolcTTSSettings(
    JSON.stringify({
      unrelated: true,
      volc_tts: {
        protocol: 'v3_http_chunked',
        resource_id: 'seed-tts-2.0',
        auth_mode: 'legacy',
        require_usage: false,
      },
    })
  )

  assert.deepEqual(defaults, {
    volc_tts_protocol: 'v3_http_chunked',
    volc_tts_resource_id: 'seed-tts-2.0',
    volc_tts_auth_mode: 'legacy',
    volc_tts_require_usage: false,
  })
})

test('serializes type 45 settings while preserving unrelated keys', () => {
  const serialized = serializeVolcTTSSettings(
    JSON.stringify({ unrelated: { enabled: true } }),
    45,
    {
      volc_tts_protocol: 'v3_ws_uni',
      volc_tts_resource_id: 'seed-icl-2.0',
      volc_tts_auth_mode: 'new_console',
      volc_tts_require_usage: true,
    }
  )

  assert.deepEqual(JSON.parse(serialized), {
    unrelated: { enabled: true },
    volc_tts: {
      protocol: 'v3_ws_uni',
      resource_id: 'seed-icl-2.0',
      auth_mode: 'new_console',
      require_usage: true,
    },
  })
})

test('removes volc_tts when changing away from type 45', () => {
  const serialized = serializeVolcTTSSettings(
    JSON.stringify({
      unrelated: true,
      volc_tts: { protocol: 'v3_ws_uni', resource_id: 'seed-tts-2.0' },
    }),
    1,
    {
      volc_tts_protocol: 'v3_ws_uni',
      volc_tts_resource_id: 'seed-tts-2.0',
      volc_tts_auth_mode: 'new_console',
      volc_tts_require_usage: true,
    }
  )

  assert.deepEqual(JSON.parse(serialized), { unrelated: true })
})

test('rejects v3 protocols without a resource id', () => {
  const result = channelFormSchema.safeParse({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'Volc TTS',
    type: 45,
    base_url: 'https://ark.cn-beijing.volces.com',
    models: 'seed-tts-2.0',
    volc_tts_protocol: 'v3_ws_uni',
    volc_tts_resource_id: '',
    volc_tts_auth_mode: 'new_console',
  })

  assert.equal(result.success, false)
  if (!result.success) {
    assert.equal(result.error.issues[0]?.path[0], 'volc_tts_resource_id')
  }
})

test('rejects legacy auth with a non appid access token key', () => {
  const result = channelFormSchema.safeParse({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'Volc TTS',
    type: 45,
    base_url: 'https://ark.cn-beijing.volces.com',
    key: 'invalid-legacy-key',
    models: 'seed-tts-2.0',
    volc_tts_protocol: 'v3_http_chunked',
    volc_tts_resource_id: 'seed-tts-2.0',
    volc_tts_auth_mode: 'legacy',
  })

  assert.equal(result.success, false)
  if (!result.success) {
    assert.equal(result.error.issues[0]?.path[0], 'key')
  }
})

test('accepts empty v1 settings for compatibility', () => {
  const result = channelFormSchema.safeParse({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'Legacy Volc TTS',
    type: 45,
    base_url: 'https://ark.cn-beijing.volces.com',
    models: 'legacy-model',
    volc_tts_protocol: 'v1_ws_binary',
    volc_tts_resource_id: '',
    volc_tts_auth_mode: 'new_console',
  })

  assert.equal(result.success, true)
})

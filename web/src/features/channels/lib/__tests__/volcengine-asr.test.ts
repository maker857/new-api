import assert from 'node:assert/strict'
import test from 'node:test'

import { CHANNEL_FORM_DEFAULT_VALUES, channelFormSchema } from '../channel-form'
import {
  parseVolcASRSettings,
  serializeVolcASRSettings,
} from '../volcengine-asr'

test('parses and serializes native volc_asr settings', () => {
  const defaults = parseVolcASRSettings(
    JSON.stringify({
      unrelated: true,
      volc_asr: {
        protocol: 'v3_auc',
        resource_id: 'volc.seedasr.auc',
        auth_mode: 'legacy',
      },
    })
  )

  assert.deepEqual(defaults, {
    volc_asr_enabled: true,
    volc_asr_protocol: 'v3_auc',
    volc_asr_resource_id: 'volc.seedasr.auc',
    volc_asr_auth_mode: 'legacy',
  })

  const serialized = serializeVolcASRSettings(
    JSON.stringify({ unrelated: true }),
    45,
    defaults
  )
  assert.deepEqual(JSON.parse(serialized), {
    unrelated: true,
    volc_asr: {
      protocol: 'v3_auc',
      resource_id: 'volc.seedasr.auc',
      auth_mode: 'legacy',
    },
  })
})

test('requires an ASR resource for native volcengine ASR', () => {
  const result = channelFormSchema.safeParse({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'Volc ASR',
    type: 45,
    base_url: 'https://ark.cn-beijing.volces.com',
    models: 'bigmodel',
    volc_asr_protocol: 'v3_auc',
    volc_asr_resource_id: '',
    volc_asr_auth_mode: 'new_console',
    volc_asr_enabled: true,
  })

  assert.equal(result.success, false)
  if (!result.success) {
    assert.equal(result.error.issues[0]?.path[0], 'volc_asr_resource_id')
  }
})

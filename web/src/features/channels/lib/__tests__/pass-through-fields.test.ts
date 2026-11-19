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
import { expect, test } from 'vitest'

import { channelSchema } from '../../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from '../channel-form'

const baseForm = {
  ...CHANNEL_FORM_DEFAULT_VALUES,
  name: 'Claude',
  type: 14,
  key: 'test-key',
  models: 'claude-opus-4-7',
}

test('enabled pass-through fields round-trip through channel settings', () => {
  const form = channelFormSchema.parse({
    ...baseForm,
    pass_through_fields_enabled: true,
    pass_through_fields: ['compaction', ' compaction ', 'context_management'],
    settings: '{"custom_option":true}',
  })
  const payload = transformFormDataToCreatePayload(form).channel
  expect(JSON.parse(payload.settings ?? '{}')).toMatchObject({
    custom_option: true,
    pass_through_fields_enabled: true,
    pass_through_fields: ['compaction', 'context_management'],
  })

  const channel = channelSchema.parse({
    ...payload,
    other: '',
    remark: '',
    id: 7,
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  const defaults = transformChannelToFormDefaults(channel)
  expect(defaults.pass_through_fields_enabled).toBe(true)
  expect(defaults.pass_through_fields).toEqual([
    'compaction',
    'context_management',
  ])
})

test('disabled switch with an empty list removes pass-through settings', () => {
  const form = channelFormSchema.parse({
    ...baseForm,
    settings: '{"pass_through_fields_enabled":false,"pass_through_fields":[]}',
  })
  const settings = JSON.parse(
    transformFormDataToCreatePayload(form).channel.settings ?? '{}'
  )
  expect(settings).not.toHaveProperty('pass_through_fields_enabled')
  expect(settings).not.toHaveProperty('pass_through_fields')
})

test.each(['model', 'stream', 'max_tokens', 'a.b'])(
  'form rejects the pass-through field %s',
  (name) => {
    const result = channelFormSchema.safeParse({
      ...baseForm,
      pass_through_fields_enabled: true,
      pass_through_fields: [name],
    })
    expect(result.success).toBe(false)
    expect(
      result.error?.issues.some(
        (issue) => issue.path[0] === 'pass_through_fields'
      )
    ).toBe(true)
  }
)

test('compaction usage switch round-trips for Anthropic and is dropped for other providers', () => {
  const claudeForm = channelFormSchema.parse({
    ...baseForm,
    claude_compaction_usage_enabled: true,
  })
  const claudePayload = transformFormDataToCreatePayload(claudeForm).channel
  expect(JSON.parse(claudePayload.settings ?? '{}')).toMatchObject({
    claude_compaction_usage_enabled: true,
  })
  const channel = channelSchema.parse({
    ...claudePayload,
    other: '',
    remark: '',
    id: 7,
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  expect(
    transformChannelToFormDefaults(channel).claude_compaction_usage_enabled
  ).toBe(true)

  const openaiForm = channelFormSchema.parse({
    ...baseForm,
    type: 1,
    settings: '{"claude_compaction_usage_enabled":true}',
  })
  const openaiSettings = JSON.parse(
    transformFormDataToCreatePayload(openaiForm).channel.settings ?? '{}'
  )
  expect(openaiSettings).not.toHaveProperty('claude_compaction_usage_enabled')
})

test('new Anthropic channels enable compaction usage by default while existing channels keep their saved value', () => {
  const newChannel = channelFormSchema.parse(baseForm)
  const newSettings = JSON.parse(
    transformFormDataToCreatePayload(newChannel).channel.settings ?? '{}'
  )
  expect(newSettings.claude_compaction_usage_enabled).toBe(true)

  const existing = channelSchema.parse({
    ...transformFormDataToCreatePayload(newChannel).channel,
    settings: '{}',
    other: '',
    remark: '',
    id: 8,
    status: 1,
    created_time: 1,
    test_time: 0,
    response_time: 0,
    balance_updated_time: 0,
  })
  expect(
    transformChannelToFormDefaults(existing).claude_compaction_usage_enabled
  ).toBe(false)
})

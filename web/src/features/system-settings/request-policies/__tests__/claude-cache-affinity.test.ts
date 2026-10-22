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
import type { TFunction } from 'i18next'
import { describe, expect, test } from 'vitest'

import {
  createRoutingPolicySchema,
  routingPolicyFormValues,
  routingPolicyOptions,
} from '../routing-form'

const t = ((key: string) => key) as unknown as TFunction

const options: Record<string, string> = {
  RetryTimes: '1',
  AutomaticRetryStatusCodes: '500-599',
  'channel_affinity_setting.enabled': 'true',
  'channel_affinity_setting.session_mode': '',
  'channel_affinity_setting.switch_on_success': 'true',
  'channel_affinity_setting.keep_on_channel_disabled': 'false',
  'channel_affinity_setting.claude_cache_only': 'true',
  'channel_affinity_setting.claude_cache_model_regex': '^claude-opus-.*$',
  'channel_affinity_setting.max_entries': '100000',
  'channel_affinity_setting.default_ttl_seconds': '3600',
  'channel_affinity_setting.rules': '[]',
}

describe('Claude cache-only affinity in the routing policy form', () => {
  test('round-trips both options through the saved policy', () => {
    const values = routingPolicyFormValues(options)
    expect(values.channel_affinity_setting.claude_cache_only).toBe(true)
    expect(values.channel_affinity_setting.claude_cache_model_regex).toBe(
      '^claude-opus-.*$'
    )
    const saved = routingPolicyOptions(values)
    expect(saved['channel_affinity_setting.claude_cache_only']).toBe('true')
    expect(saved['channel_affinity_setting.claude_cache_model_regex']).toBe(
      '^claude-opus-.*$'
    )
  })

  test('falls back to the Claude model scope when the regex is unset', () => {
    const values = routingPolicyFormValues({
      ...options,
      'channel_affinity_setting.claude_cache_only': '',
      'channel_affinity_setting.claude_cache_model_regex': '',
    })
    expect(values.channel_affinity_setting.claude_cache_only).toBe(false)
    expect(values.channel_affinity_setting.claude_cache_model_regex).toBe(
      '^claude-.*$'
    )
  })

  test('rejects an empty model regex before saving', () => {
    const values = routingPolicyFormValues(options)
    values.channel_affinity_setting.claude_cache_model_regex = '  '
    expect(createRoutingPolicySchema(t).safeParse(values).success).toBe(false)
  })
})

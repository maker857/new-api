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
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { ChannelAffinitySection } from '..'
import { SettingsPageProvider } from '../../../components/settings-page-context'
import type { ChannelAffinitySettings } from '../types'

const defaults: ChannelAffinitySettings = {
  'channel_affinity_setting.enabled': true,
  'channel_affinity_setting.switch_on_success': true,
  'channel_affinity_setting.keep_on_channel_disabled': false,
  'channel_affinity_setting.claude_cache_only': false,
  'channel_affinity_setting.claude_cache_model_regex': '^claude-.*$',
  'channel_affinity_setting.max_entries': 100000,
  'channel_affinity_setting.default_ttl_seconds': 3600,
  'channel_affinity_setting.rules': '[]',
}

describe('Claude cache affinity settings', () => {
  let queryClient: QueryClient
  let actions: HTMLDivElement

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false } },
    })
    actions = document.createElement('div')
    document.body.append(actions)
    vi.spyOn(api, 'get').mockResolvedValue({ data: { success: true } })
  })

  afterEach(() => {
    queryClient.clear()
    actions.remove()
  })

  function renderSettings(values = defaults) {
    return render(
      <QueryClientProvider client={queryClient}>
        <SettingsPageProvider actionsContainer={actions}>
          <ChannelAffinitySection defaultValues={values} />
        </SettingsPageProvider>
      </QueryClientProvider>
    )
  }

  test('saves the model scope before enabling cache-only affinity without editing rules', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    renderSettings()
    const toggle = screen.getByRole('switch', {
      name: 'Claude: use affinity only for cached requests',
    })
    expect(toggle).not.toBeChecked()
    fireEvent.click(toggle)
    fireEvent.click(
      screen.getByRole('button', {
        name: 'Claude: use affinity only for cached requests',
      })
    )
    fireEvent.change(
      screen.getByRole('textbox', {
        name: 'Claude cache affinity model regex',
      }),
      {
        target: { value: '^(claude-.*|my-claude)$' },
      }
    )
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() => expect(put).toHaveBeenCalledTimes(2))
    expect(put).toHaveBeenNthCalledWith(1, '/api/option/', {
      key: 'channel_affinity_setting.claude_cache_model_regex',
      value: '^(claude-.*|my-claude)$',
    })
    expect(put).toHaveBeenNthCalledWith(2, '/api/option/', {
      key: 'channel_affinity_setting.claude_cache_only',
      value: 'true',
    })
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    )
  })

  test('loads a saved policy and allows disabling it without changing its scope', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockResolvedValue({ data: { success: true } })
    renderSettings({
      ...defaults,
      'channel_affinity_setting.claude_cache_only': true,
    })
    const toggle = screen.getByRole('switch', {
      name: 'Claude: use affinity only for cached requests',
    })
    expect(toggle).toBeChecked()
    fireEvent.click(toggle)
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(put).toHaveBeenCalledExactlyOnceWith('/api/option/', {
        key: 'channel_affinity_setting.claude_cache_only',
        value: 'false',
      })
    )
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    )
  })

  test('rejects an empty scope and never enables a policy after server regex validation fails', async () => {
    const put = vi.spyOn(api, 'put').mockResolvedValue({
      data: { success: false, message: 'Invalid Go regex' },
    })
    renderSettings()
    fireEvent.click(
      screen.getByRole('button', {
        name: 'Claude: use affinity only for cached requests',
      })
    )
    fireEvent.click(
      screen.getByRole('switch', {
        name: 'Claude: use affinity only for cached requests',
      })
    )
    const scope = screen.getByRole('textbox', {
      name: 'Claude cache affinity model regex',
    })
    fireEvent.change(scope, { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(put).not.toHaveBeenCalled()
    fireEvent.change(scope, { target: { value: '[' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    await waitFor(() =>
      expect(screen.getByRole('button', { name: 'Save' })).toBeEnabled()
    )
    expect(put).toHaveBeenCalledExactlyOnceWith('/api/option/', {
      key: 'channel_affinity_setting.claude_cache_model_regex',
      value: '[',
    })
  })

  test('starts collapsed and lets the switch change independently of the details', async () => {
    const user = userEvent.setup()
    renderSettings()
    const details = screen.getByRole('button', {
      name: 'Claude: use affinity only for cached requests',
    })
    const toggle = screen.getByRole('switch', {
      name: 'Claude: use affinity only for cached requests',
    })
    expect(details).toHaveAttribute('aria-expanded', 'false')
    expect(
      screen.queryByRole('textbox', {
        name: 'Claude cache affinity model regex',
      })
    ).not.toBeInTheDocument()
    await user.click(toggle)
    expect(toggle).toBeChecked()
    expect(details).toHaveAttribute('aria-expanded', 'false')

    details.focus()
    await user.keyboard('{Enter}')
    expect(details).toHaveAttribute('aria-expanded', 'true')
    const scope = screen.getByRole('textbox', {
      name: 'Claude cache affinity model regex',
    })
    expect(scope).toBeVisible()
    fireEvent.change(scope, { target: { value: '^my-claude$' } })
    expect(toggle).toBeChecked()

    details.focus()
    await user.keyboard(' ')
    expect(details).toHaveAttribute('aria-expanded', 'false')
    expect(
      screen.queryByRole('textbox', {
        name: 'Claude cache affinity model regex',
      })
    ).not.toBeInTheDocument()
    await user.click(details)
    expect(
      screen.getByRole('textbox', { name: 'Claude cache affinity model regex' })
    ).toHaveValue('^my-claude$')
  })

  test('preserves unsaved cache policy edits when unrelated settings are refreshed', () => {
    const view = renderSettings()
    fireEvent.click(
      screen.getByRole('button', {
        name: 'Claude: use affinity only for cached requests',
      })
    )
    fireEvent.click(
      screen.getByRole('switch', {
        name: 'Claude: use affinity only for cached requests',
      })
    )
    fireEvent.change(
      screen.getByRole('textbox', {
        name: 'Claude cache affinity model regex',
      }),
      { target: { value: '^(claude-.*|custom-alias)$' } }
    )
    view.rerender(
      <QueryClientProvider client={queryClient}>
        <SettingsPageProvider actionsContainer={actions}>
          <ChannelAffinitySection
            defaultValues={{
              ...defaults,
              'channel_affinity_setting.max_entries': 200000,
            }}
          />
        </SettingsPageProvider>
      </QueryClientProvider>
    )
    expect(
      screen.getByRole('switch', {
        name: 'Claude: use affinity only for cached requests',
      })
    ).toBeChecked()
    expect(
      screen.getByRole('textbox', {
        name: 'Claude cache affinity model regex',
      })
    ).toHaveValue('^(claude-.*|custom-alias)$')
  })
})

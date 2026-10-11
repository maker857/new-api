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
import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'

import {
  DiagnosticUsageStatus,
  DiagnosticUsageValue,
} from '../diagnostic-usage-status'

describe('Diagnostic capture usage status', () => {
  test('first initialization shows unknown usage instead of an empty disk', () => {
    render(
      <DiagnosticUsageValue
        initialized={false}
        estimated={false}
        value='0 Bytes'
      />
    )
    expect(screen.getByText('Not yet available')).toBeInTheDocument()
    expect(screen.queryByText('0 Bytes')).not.toBeInTheDocument()
  })

  test('a verified empty directory shows zero and an estimate stays labeled', () => {
    const view = render(
      <DiagnosticUsageValue initialized estimated={false} value='0 Bytes' />
    )
    expect(screen.getByText('0 Bytes')).toBeInTheDocument()
    view.rerender(<DiagnosticUsageValue initialized estimated value='12 GB' />)
    expect(screen.getByText('12 GB')).toBeInTheDocument()
    expect(screen.getByText(/Estimated/)).toBeInTheDocument()
  })

  test('restart recovery and saving failure remain visible together', () => {
    render(
      <DiagnosticUsageStatus
        info={{
          usage_initialized: true,
          usage_restored: true,
          usage_save_failed: true,
          usage_last_saved_at: 100,
        }}
      />
    )
    expect(screen.getByRole('status')).toHaveTextContent('Usage restored')
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Usage statistics could not be saved'
    )
    expect(screen.getByText('Last successful save')).toBeInTheDocument()
  })

  test('scan progress and reconciliation failure remain visible', () => {
    const view = render(
      <DiagnosticUsageStatus
        info={{
          usage_initialized: true,
          usage_scanning: true,
        }}
      />
    )
    expect(screen.getByRole('status')).toHaveTextContent('Reconciling usage')
    view.rerender(
      <DiagnosticUsageStatus
        info={{
          usage_initialized: true,
          usage_scan_failed: true,
          usage_scan_pending: true,
        }}
      />
    )
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Reconciliation failed; retry scheduled'
    )
  })

  test('failed status requests are not presented as zero usage', () => {
    render(<DiagnosticUsageStatus info={null} unavailable />)
    expect(screen.getByRole('alert')).toHaveTextContent(
      'Usage status could not be refreshed'
    )
    expect(screen.queryByText('0 Bytes')).not.toBeInTheDocument()
  })
})

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
import { useTranslation } from 'react-i18next'

import { formatTimestampToDate } from '@/lib/format'

export type DiagnosticUsageInfo = {
  usage_initialized?: boolean
  usage_estimated?: boolean
  usage_restored?: boolean
  usage_scanning?: boolean
  usage_scan_pending?: boolean
  usage_save_failed?: boolean
  usage_scan_failed?: boolean
  usage_last_saved_at?: number
  usage_last_reconciled_at?: number
  usage_next_reconciliation_at?: number
}

export function DiagnosticUsageValue(props: {
  initialized?: boolean
  estimated?: boolean
  value: string
}) {
  const { t } = useTranslation()
  if (!props.initialized) return <span>{t('Not yet available')}</span>
  return (
    <span>
      {props.value}
      {props.estimated && (
        <span className='text-muted-foreground'> ({t('Estimated')})</span>
      )}
    </span>
  )
}

export function DiagnosticUsageStatus(props: {
  info: DiagnosticUsageInfo | null
  unavailable?: boolean
}) {
  const { t } = useTranslation()
  const info = props.info
  let status = t('Usage tracking active')
  if (!info) {
    status = t('Loading usage statistics')
  } else if (!info.usage_initialized) {
    status = t('Building initial usage statistics')
  } else if (info.usage_scanning) {
    status = t('Reconciling usage')
  } else if (info.usage_scan_pending) {
    status = t('Reconciliation queued')
  } else if (info.usage_restored) {
    status = t('Usage restored')
  }

  return (
    <div className='bg-background flex flex-col gap-3 rounded-xl border p-4 text-sm'>
      <p role='status'>{status}</p>
      {props.unavailable && (
        <p role='alert' className='text-destructive'>
          {t('Usage status could not be refreshed')}
        </p>
      )}
      {info?.usage_save_failed && (
        <p role='alert' className='text-destructive'>
          {t('Usage statistics could not be saved')}
        </p>
      )}
      {info?.usage_scan_failed && (
        <p role='alert' className='text-destructive'>
          {t('Reconciliation failed; retry scheduled')}
        </p>
      )}
      {info && (
        <dl className='grid gap-3 sm:grid-cols-3'>
          <div>
            <dt className='text-muted-foreground'>
              {t('Last successful save')}
            </dt>
            <dd>
              {info.usage_last_saved_at
                ? formatTimestampToDate(info.usage_last_saved_at)
                : '—'}
            </dd>
          </div>
          <div>
            <dt className='text-muted-foreground'>
              {t('Last usage reconciliation')}
            </dt>
            <dd>
              {info.usage_last_reconciled_at
                ? formatTimestampToDate(info.usage_last_reconciled_at)
                : '—'}
            </dd>
          </div>
          <div>
            <dt className='text-muted-foreground'>
              {t('Next usage reconciliation')}
            </dt>
            <dd>
              {info.usage_next_reconciliation_at
                ? formatTimestampToDate(info.usage_next_reconciliation_at)
                : '—'}
            </dd>
          </div>
        </dl>
      )}
    </div>
  )
}

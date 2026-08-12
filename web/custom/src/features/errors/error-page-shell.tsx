import { ArrowLeft, Home } from 'lucide-react'
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
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

type ErrorPageShellProps = {
  status: number
  title: string
  description: ReactNode
  icon: ReactNode
  className?: string
  minimal?: boolean
  onBack?: () => void
  onHome?: () => void
}

export function ErrorPageShell({
  status,
  title,
  description,
  icon,
  className,
  minimal = false,
  onBack,
  onHome,
}: ErrorPageShellProps) {
  const { t } = useTranslation()

  return (
    <div
      className={cn(
        'from-background via-muted/30 to-background flex min-h-svh w-full items-center justify-center bg-linear-to-br px-4 py-10',
        className
      )}
    >
      <div className='border-border/70 bg-card/88 w-full max-w-[540px] rounded-[28px] border px-6 py-7 text-center shadow-[0_24px_80px_rgba(15,23,42,0.08)] backdrop-blur sm:px-10 sm:py-9'>
        {!minimal && (
          <div className='mb-6 flex justify-center'>
            <div className='bg-muted/75 text-muted-foreground border-border/70 inline-flex items-center gap-2 rounded-full border px-3 py-1 text-xs font-medium'>
              <span>{t('KalosAI service status')}</span>
              <span className='bg-border h-3 w-px' aria-hidden='true' />
              <span>{status}</span>
            </div>
          </div>
        )}
        <div className='mx-auto mb-5 flex size-14 items-center justify-center rounded-2xl border border-slate-200/70 bg-slate-50 text-slate-700 shadow-sm dark:border-slate-700/70 dark:bg-slate-900/70 dark:text-slate-200'>
          {icon}
        </div>
        {!minimal && (
          <div className='text-muted-foreground/20 mb-1 text-[4.5rem] leading-none font-semibold tracking-normal sm:text-[5.5rem]'>
            {status}
          </div>
        )}
        <h1 className='text-foreground text-2xl font-semibold tracking-normal'>
          {title}
        </h1>
        <div className='text-muted-foreground mx-auto mt-3 max-w-[360px] text-sm leading-6'>
          {description}
        </div>
        {!minimal && (
          <div className='mt-7 flex flex-col justify-center gap-3 sm:flex-row'>
            {onBack && (
              <Button variant='outline' onClick={onBack}>
                <ArrowLeft className='size-4' aria-hidden='true' />
                {t('Go Back')}
              </Button>
            )}
            {onHome && (
              <Button onClick={onHome}>
                <Home className='size-4' aria-hidden='true' />
                {t('Back to Home')}
              </Button>
            )}
          </div>
        )}
      </div>
    </div>
  )
}

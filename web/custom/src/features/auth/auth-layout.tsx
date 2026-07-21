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
import { Link } from '@tanstack/react-router'

import { Skeleton } from '@/components/ui/skeleton'
import { useSystemConfig } from '@/hooks/use-system-config'
import { DEFAULT_LOGO } from '@/lib/constants'

type AuthLayoutProps = {
  children: React.ReactNode
}

export function AuthLayout({ children }: AuthLayoutProps) {
  const { systemName, logo, logoLoaded, loading } = useSystemConfig()

  const displayName = systemName === 'New API' ? 'Kalos' : systemName
  const customLogo = Boolean(logo && logo !== DEFAULT_LOGO && logoLoaded)
  const brandMark =
    customLogo && !loading ? (
      <img
        src={logo}
        alt={displayName}
        className='size-full rounded-[inherit] object-cover'
      />
    ) : (
      <span
        aria-hidden='true'
        className='bg-foreground text-background flex size-full items-center justify-center rounded-[inherit] text-sm font-semibold'
      >
        K
      </span>
    )

  return (
    <div className='kalos-auth-page relative min-h-svh overflow-hidden'>
      <Link
        to='/'
        className='absolute top-4 left-4 z-10 flex items-center gap-2 rounded-md px-2 py-1 transition-colors hover:bg-black/5 sm:top-8 sm:left-8'
      >
        <div className='relative size-8 rounded-lg'>
          {loading ? (
            <Skeleton className='absolute inset-0 rounded-lg' />
          ) : (
            brandMark
          )}
        </div>
        {loading ? (
          <Skeleton className='h-6 w-24' />
        ) : (
          <h1 className='text-xl font-semibold tracking-normal'>
            {displayName}
          </h1>
        )}
      </Link>

      <div className='relative z-[1] container grid min-h-svh items-center gap-10 px-4 py-24 lg:grid-cols-[1fr_460px] lg:px-8 lg:py-16 xl:gap-16'>
        <section className='hidden max-w-2xl lg:block'>
          <div className='mb-8 inline-flex items-center gap-2 rounded-full border border-black/10 bg-white/70 px-3 py-1 text-xs font-medium text-neutral-600 shadow-sm'>
            <span className='size-1.5 rounded-full bg-neutral-950' />
            AI Gateway for Kalos
          </div>
          <h2 className='max-w-[720px] text-5xl leading-[1.05] font-semibold tracking-normal text-neutral-950 xl:text-6xl'>
            统一管理模型调用、令牌、用量与账单。
          </h2>
          <p className='mt-6 max-w-xl text-base leading-7 text-neutral-600'>
            从一个清晰稳定的控制台进入
            Kalos。模型接入、密钥分发、调用日志和成本统计都保持在同一套工作流里。
          </p>

          <div className='mt-12 grid max-w-xl grid-cols-2 gap-3'>
            {['真实模型目录', '统一令牌', '调用日志', '透明计费'].map(
              (item) => (
                <div
                  key={item}
                  className='rounded-xl border border-black/8 bg-white/64 px-4 py-3 text-sm font-medium text-neutral-800 shadow-[0_1px_2px_rgb(0_0_0/0.04)]'
                >
                  {item}
                </div>
              )
            )}
          </div>
        </section>

        <div className='mx-auto w-full max-w-[460px]'>
          <div className='rounded-2xl border border-black/10 bg-white/86 p-6 shadow-[0_18px_60px_rgb(0_0_0/0.08)] backdrop-blur-xl sm:p-8'>
            {children}
          </div>
        </div>
      </div>
    </div>
  )
}

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
import { ArrowRight, BookOpen } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { KALOS_PUBLIC_ORIGIN, usePublicSiteData } from '@/features/home/hooks'
import { cn } from '@/lib/utils'

interface HeroProps {
  className?: string
  isAuthenticated?: boolean
}

function getHost(address: string) {
  try {
    return new URL(address).host
  } catch {
    return address.replace(/^https?:\/\//, '')
  }
}

function getPercentValue(label: string) {
  const matched = label.match(/[\d.]+/)
  if (!matched) return 0
  return Math.min(Number(matched[0]) || 0, 100)
}

export function Hero(props: HeroProps) {
  const { selectedModels } = usePublicSiteData()
  const serverAddress = KALOS_PUBLIC_ORIGIN

  const visibleModels =
    selectedModels.length > 0
      ? selectedModels
      : [
          {
            name: 'gpt-4o',
            vendor: 'OpenAI',
            endpoint: 'OpenAI Chat',
            rank: undefined,
            share: undefined,
            callsLabel: '加载中',
          },
        ]

  return (
    <section className='border-border/70 relative border-b px-4 pt-24 pb-14 md:px-6 md:pt-32 md:pb-20'>
      <div
        aria-hidden
        className='pointer-events-none absolute inset-0 -z-10 bg-[linear-gradient(to_right,var(--border)_1px,transparent_1px),linear-gradient(to_bottom,var(--border)_1px,transparent_1px)] bg-[size:56px_56px] opacity-[0.14]'
      />
      <div className='mx-auto grid max-w-7xl gap-10 lg:grid-cols-[minmax(0,1fr)_540px] lg:items-center'>
        <div>
          <div
            className='landing-animate-fade-up border-border/70 bg-muted/30 text-muted-foreground inline-flex items-center rounded-md border px-3 py-1.5 text-xs opacity-0 shadow-xs'
            style={{ animationDelay: '0ms' }}
          >
            Kalos 真实模型目录 · 按周调用热度排序
          </div>

          <h1
            className='landing-animate-fade-up mt-6 max-w-4xl text-[clamp(2.35rem,5vw,4.65rem)] leading-[1.08] font-bold tracking-normal opacity-0'
            style={{ animationDelay: '70ms' }}
          >
            一个接口，
            <br />
            调用全站真实可用模型。
          </h1>

          <p
            className='landing-animate-fade-up text-muted-foreground mt-6 max-w-2xl text-base leading-8 opacity-0 md:text-lg'
            style={{ animationDelay: '140ms' }}
          >
            聚合 GPT、Claude、Gemini、DeepSeek、智谱、Moonshot
            等模型能力，用统一令牌、统一计费、统一日志支撑你的应用接入。
          </p>

          <div
            className='landing-animate-fade-up mt-8 flex flex-wrap gap-3 opacity-0'
            style={{ animationDelay: '210ms' }}
          >
            {props.isAuthenticated ? (
              <Button
                className='h-10 rounded-md px-4 text-sm'
                render={<Link to='/dashboard' />}
              >
                进入控制台
                <ArrowRight className='ml-2 size-4' />
              </Button>
            ) : (
              <Button
                className='h-10 rounded-md px-4 text-sm'
                render={<Link to='/sign-up' />}
              >
                立即注册
                <ArrowRight className='ml-2 size-4' />
              </Button>
            )}
            <Button
              variant='outline'
              className='border-border/70 bg-muted/25 hover:bg-muted/45 h-10 rounded-md px-4 text-sm'
              render={<Link to='/pricing' />}
            >
              查看模型价格
            </Button>
            <Button
              variant='outline'
              className='border-border/70 bg-muted/25 hover:bg-muted/45 h-10 rounded-md px-4 text-sm'
              render={
                <a
                  href={`${serverAddress}/pricing`}
                  target='_blank'
                  rel='noopener noreferrer'
                />
              }
            >
              <BookOpen className='mr-2 size-4' />
              模型广场
            </Button>
          </div>

          <div
            className='landing-animate-fade-up mt-10 grid max-w-2xl grid-cols-2 gap-3 opacity-0 sm:grid-cols-4'
            style={{ animationDelay: '280ms' }}
          >
            {['OpenAI', 'Anthropic', 'Google', 'DeepSeek'].map((name) => (
              <div
                key={name}
                className='border-border/70 bg-muted/25 rounded-md border px-3 py-2 text-sm font-medium shadow-xs'
              >
                {name}
              </div>
            ))}
          </div>
        </div>

        <div
          className='landing-animate-fade-up opacity-0 lg:pl-2'
          style={{ animationDelay: '320ms' }}
        >
          <div className='border-border/80 overflow-hidden rounded-lg border bg-[#fcfcfd] shadow-sm'>
            <div className='border-border/70 bg-muted/20 flex items-start justify-between gap-4 border-b px-5 py-4'>
              <div>
                <div className='text-sm font-semibold'>Kalos 模型路由</div>
                <div className='text-muted-foreground mt-1 text-xs'>
                  按近期调用热度展示当前可用模型。
                </div>
              </div>
              <div className='border-border/70 bg-background/70 text-muted-foreground rounded-md border px-2 py-1 text-[11px]'>
                {getHost(serverAddress)}
              </div>
            </div>

            <div className='divide-border/60 divide-y'>
              {visibleModels.map((model, index) => {
                const percent = getPercentValue(model.callsLabel)

                return (
                  <div
                    key={model.name}
                    className='hover:bg-background/70 grid grid-cols-[minmax(0,1fr)_116px] gap-5 px-5 py-4 text-sm transition-colors'
                  >
                    <div className='min-w-0'>
                      <div className='flex items-center gap-2.5'>
                        <span
                          className={cn(
                            'text-muted-foreground flex size-5 shrink-0 items-center justify-center rounded-md border text-[11px] font-medium',
                            index < 3 &&
                              'border-foreground/15 bg-foreground text-background'
                          )}
                        >
                          {model.rank || index + 1}
                        </span>
                        <div className='truncate text-[15px] font-semibold tracking-normal'>
                          {model.name}
                        </div>
                      </div>
                      <div className='bg-muted mt-3 h-1.5 overflow-hidden rounded-full'>
                        <div
                          className='bg-foreground h-full rounded-full'
                          style={{ width: `${Math.max(percent, 4)}%` }}
                        />
                      </div>
                    </div>
                    <div className='text-right'>
                      <div className='text-muted-foreground text-[11px]'>
                        周调用占比
                      </div>
                      <div className='text-foreground mt-1 text-sm font-semibold tabular-nums'>
                        {model.callsLabel}
                      </div>
                    </div>
                  </div>
                )
              })}
            </div>

            <div className='border-border/70 bg-muted/15 text-muted-foreground flex items-center justify-between gap-4 border-t px-5 py-3 text-xs'>
              <span>数据随模型目录与调用排行自动更新</span>
              <span className='tabular-nums'>
                {visibleModels.length} models
              </span>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}

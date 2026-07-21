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
import { Activity, Gauge, Timer } from 'lucide-react'

import { usePublicSiteData } from '@/features/home/hooks'

function formatNumber(value: number) {
  if (!value) return '暂无样本'
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 10_000) return `${(value / 10_000).toFixed(1)}万`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return `${value}`
}

function formatTokens(value: number) {
  if (!value) return '0'
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}K`
  return `${value}`
}

function formatPercent(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value)) return '暂无样本'
  return `${value.toFixed(1)}%`
}

function formatLatency(value: number | null | undefined) {
  if (value == null || !Number.isFinite(value) || value <= 0)
    return '暂无样本'
  return `${Math.round(value)}ms`
}

function getModelVendor(modelName: string) {
  const normalized = modelName.toLowerCase()
  if (normalized.includes('claude')) return 'anthropic'
  if (normalized.includes('gpt') || normalized.includes('openai'))
    return 'openai'
  if (normalized.includes('gemini')) return 'google'
  if (normalized.includes('deepseek')) return 'deepseek'
  if (normalized.includes('glm')) return 'zhipu'
  if (normalized.includes('qwen')) return 'alibaba'
  return 'kalos'
}

function getModelInitial(modelName: string) {
  const vendor = getModelVendor(modelName)
  if (vendor === 'openai') return '◎'
  if (vendor === 'google') return 'G'
  if (vendor === 'anthropic') return 'AI'
  return modelName.slice(0, 2).toUpperCase()
}

export function UsageOverview() {
  const { isLoading, perfModels } = usePublicSiteData()

  const topModels = perfModels
    .filter((model) => (model.request_count || 0) > 0)
    .sort(
      (a, b) =>
        (b.request_count || 0) - (a.request_count || 0) ||
        (b.output_tokens || 0) - (a.output_tokens || 0)
    )
    .slice(0, 3)

  return (
    <section className='border-border/70 border-b px-4 py-12 md:px-6 md:py-16'>
      <div className='mx-auto max-w-7xl'>
        <div className='mb-7 flex items-end justify-between gap-4'>
          <div>
            <h2 className='text-3xl leading-tight font-semibold tracking-normal md:text-5xl'>
              热门模型
            </h2>
            <p className='text-muted-foreground mt-2 text-sm'>
              本站过去 30 天调用量前三的模型
            </p>
          </div>
          <div className='text-muted-foreground hidden text-sm md:block'>
            按调用量排序
          </div>
        </div>

        {topModels.length > 0 ? (
          <div className='grid gap-4 md:grid-cols-3'>
            {topModels.map((model, index) => (
              <div
                key={model.model_name}
                className='border-border/70 bg-background/70 rounded-lg border p-6'
              >
                <div className='flex items-start justify-between gap-4'>
                  <div className='flex min-w-0 items-center gap-3'>
                    <div className='border-border/70 bg-muted/40 flex size-12 shrink-0 items-center justify-center rounded-full border text-sm font-semibold'>
                      {getModelInitial(model.model_name)}
                    </div>
                    <div className='min-w-0'>
                      <h3 className='truncate text-lg leading-tight font-semibold'>
                        {model.model_name}
                      </h3>
                      <p className='text-muted-foreground mt-1 text-sm'>
                        by {getModelVendor(model.model_name)}
                      </p>
                    </div>
                  </div>
                  <div className='text-muted-foreground rounded-md border px-2 py-1 text-xs'>
                    #{index + 1}
                  </div>
                </div>

                <div className='border-border/70 mt-5 border-t pt-5'>
                  <div className='grid grid-cols-2 gap-5'>
                    <div>
                      <div className='text-muted-foreground flex items-center gap-1.5 text-sm'>
                        <Activity className='size-3.5' />
                        调用量
                      </div>
                      <div className='mt-1 text-xl font-semibold tabular-nums'>
                        {isLoading
                          ? '...'
                          : formatNumber(model.request_count || 0)}
                      </div>
                    </div>
                    <div>
                      <div className='text-muted-foreground text-right text-sm'>
                        输出 Token
                      </div>
                      <div className='mt-1 text-right text-xl font-semibold tabular-nums'>
                        {isLoading
                          ? '...'
                          : formatTokens(model.output_tokens || 0)}
                      </div>
                    </div>
                  </div>

                  <div className='mt-5 grid grid-cols-2 gap-3 text-sm'>
                    <div className='border-border/60 rounded-md border px-3 py-2.5'>
                      <div className='text-muted-foreground flex items-center gap-1.5'>
                        <Gauge className='size-3.5' />
                        成功率
                      </div>
                      <div className='mt-1 font-medium tabular-nums'>
                        {isLoading ? '...' : formatPercent(model.success_rate)}
                      </div>
                    </div>
                    <div className='border-border/60 rounded-md border px-3 py-2.5'>
                      <div className='text-muted-foreground flex items-center gap-1.5'>
                        <Timer className='size-3.5' />
                        延迟
                      </div>
                      <div className='mt-1 font-medium tabular-nums'>
                        {isLoading
                          ? '...'
                          : formatLatency(model.avg_ttft_ms)}
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className='text-muted-foreground rounded-lg border border-dashed p-10 text-center text-sm'>
            暂无 30 天模型调用样本
          </div>
        )}
      </div>
    </section>
  )
}

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
import {
  ArrowUpRight,
  BarChart3,
  ChevronRight,
  KeyRound,
  Layers3,
  Route,
  ShieldCheck,
} from 'lucide-react'
import type { CSSProperties } from 'react'

import { Button } from '@/components/ui/button'
import { KALOS_PUBLIC_ORIGIN, usePublicSiteData } from '@/features/home/hooks'

interface ApplePreviewHomeProps {
  isAuthenticated?: boolean
}

const glassStyle: CSSProperties = {
  backdropFilter: 'blur(22px) saturate(170%)',
  WebkitBackdropFilter: 'blur(22px) saturate(170%)',
}

function getHost(address: string) {
  try {
    return new URL(address).host
  } catch {
    return address.replace(/^https?:\/\//, '')
  }
}

function formatMetric(value: number, isLoading: boolean) {
  if (isLoading) return '...'
  return `${value || 0}`
}

function getShareWidth(share?: number) {
  if (!share || share <= 0) return '3%'
  return `${Math.max(3, Math.min(100, share * 100))}%`
}

export function ApplePreviewHome(props: ApplePreviewHomeProps) {
  const {
    isLoading,
    selectedModels,
    models,
    vendors,
    groups,
    endpoints,
    vendorRows,
  } = usePublicSiteData()

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
            callsLabel: isLoading ? '更新中' : '暂无调用',
          },
        ]

  const metrics = [
    [formatMetric(models.length, isLoading), '公开模型'],
    [formatMetric(vendors.length, isLoading), '供应商'],
    [formatMetric(groups.length, isLoading), '可用分组'],
    [formatMetric(endpoints.length, isLoading), '兼容接口'],
  ]

  const features = [
    {
      icon: <Route className='size-6' />,
      title: '一个入口',
      desc: '统一调度 GPT、Claude、Gemini、DeepSeek、智谱、Moonshot 等模型。',
    },
    {
      icon: <KeyRound className='size-6' />,
      title: '统一令牌',
      desc: '为不同应用创建独立 Key，分别管理额度、过期时间和可用模型。',
    },
    {
      icon: <BarChart3 className='size-6' />,
      title: '真实排行',
      desc: '模型热度来自 Kalos 当前公开调用数据，刷新后自动更新。',
    },
    {
      icon: <ShieldCheck className='size-6' />,
      title: '稳定接入',
      desc: '用统一日志、统一计费和分组权限支撑生产环境调用。',
    },
  ]

  return (
    <main className='apple-preview min-h-screen overflow-hidden bg-[#f5f5f7] text-[#1d1d1f]'>
      <section className='relative isolate overflow-hidden px-5 pt-28 pb-12 text-center md:px-8 md:pt-32 md:pb-16'>
        <div
          aria-hidden
          className='absolute inset-0 -z-20 bg-[linear-gradient(180deg,#fff_0%,#f7f7fa_58%,#f0f0f4_100%)]'
        />
        <div
          aria-hidden
          className='absolute top-20 left-1/2 -z-10 h-[460px] w-[900px] -translate-x-1/2 rounded-full bg-[radial-gradient(circle,rgba(0,113,227,0.14)_0%,rgba(94,92,230,0.08)_34%,transparent_68%)] blur-3xl'
        />

        <div className='mx-auto max-w-6xl'>
          <div
            style={glassStyle}
            className='mx-auto mb-6 inline-flex items-center rounded-full border border-white/80 bg-white/70 px-4 py-2 text-sm font-medium text-[#424245] shadow-[0_8px_28px_rgba(0,0,0,0.06)]'
          >
            Kalos AI 模型聚合平台
          </div>
          <h1 className='mx-auto text-[clamp(4rem,8.4vw,8rem)] leading-[0.9] font-semibold tracking-[-0.06em] text-balance'>
            Kalos AI
          </h1>
          <p className='mx-auto mt-5 max-w-5xl text-[clamp(2.15rem,4.6vw,4.8rem)] leading-[0.98] font-semibold tracking-[-0.055em] text-balance'>
            一个接口，调用全站模型。
          </p>
          <p className='mx-auto mt-7 max-w-3xl text-[clamp(1.18rem,2vw,1.55rem)] leading-8 font-medium tracking-[-0.01em] text-balance text-[#6e6e73]'>
            聚合多家 AI 模型能力，使用统一令牌、统一计费和统一日志，
            为你的应用提供稳定的 OpenAI 兼容接入。
          </p>
          <div className='mt-8 flex flex-wrap items-center justify-center gap-x-7 gap-y-4 text-[21px] font-medium text-[#06c]'>
            <Link
              to={props.isAuthenticated ? '/dashboard' : '/sign-up'}
              className='inline-flex items-center gap-1 hover:underline'
            >
              {props.isAuthenticated ? '进入控制台' : '立即注册'}
              <ChevronRight className='size-5' />
            </Link>
            <Link
              to='/pricing'
              className='inline-flex items-center gap-1 hover:underline'
            >
              查看模型价格
              <ChevronRight className='size-5' />
            </Link>
          </div>

          <div className='mx-auto mt-12 grid max-w-4xl grid-cols-2 gap-px overflow-hidden rounded-[28px] bg-[#d2d2d7] text-left shadow-[0_18px_54px_rgba(0,0,0,0.07)] md:grid-cols-4'>
            {metrics.map(([value, label]) => (
              <div key={label} className='bg-white/82 px-6 py-6'>
                <div className='text-4xl font-semibold tracking-[-0.045em] tabular-nums'>
                  {value}
                </div>
                <div className='mt-2 text-sm font-medium text-[#6e6e73]'>
                  {label}
                </div>
              </div>
            ))}
          </div>
        </div>
      </section>

      <section className='px-4 pb-4 md:px-6'>
        <div className='mx-auto grid max-w-[1400px] gap-4 lg:grid-cols-2'>
          <article className='min-h-[590px] overflow-hidden rounded-[34px] bg-white px-7 pt-11 text-center shadow-[0_1px_0_rgba(0,0,0,0.04)] md:px-12'>
            <p className='text-xl font-semibold tracking-[-0.02em] text-[#6e6e73]'>
              Kalos 模型路由
            </p>
            <h2 className='mx-auto mt-2 max-w-2xl text-[clamp(2.6rem,4.7vw,4.9rem)] leading-[0.96] font-semibold tracking-[-0.055em]'>
              每一次请求，
              <br />
              都找到合适模型。
            </h2>
            <p className='mx-auto mt-5 max-w-xl text-xl leading-8 font-medium text-[#6e6e73]'>
              以 {getHost(KALOS_PUBLIC_ORIGIN)} 作为统一入口，保留 OpenAI
              兼容调用方式。
            </p>
            <div className='mx-auto mt-9 max-w-2xl rounded-[30px] bg-[#1d1d1f] p-5 text-left text-white shadow-[0_26px_70px_rgba(0,0,0,0.2)]'>
              <div className='mb-4 flex items-center justify-between gap-4'>
                <div className='flex items-center gap-2 text-sm font-semibold text-white/60'>
                  <Layers3 className='size-4' />
                  API Request
                </div>
                <div className='rounded-full bg-white/10 px-3 py-1 text-xs font-semibold text-white/58'>
                  OpenAI compatible
                </div>
              </div>
              <pre className='overflow-x-auto text-sm leading-7 text-white/78'>
                <code>{`curl ${KALOS_PUBLIC_ORIGIN}/v1/chat/completions \\
  -H "Authorization: Bearer $API_KEY" \\
  -d '{"model":"gpt-5.5","messages":[{"role":"user","content":"你好"}]}'`}</code>
              </pre>
              <div className='mt-5 grid grid-cols-3 gap-2 text-center text-xs font-semibold text-white/62'>
                {['路由', '计费', '日志'].map((item) => (
                  <div key={item} className='rounded-2xl bg-white/[0.08] py-3'>
                    {item}
                  </div>
                ))}
              </div>
            </div>
          </article>

          <article className='min-h-[590px] overflow-hidden rounded-[34px] bg-[#fbfbfd] px-7 pt-11 text-center shadow-[0_1px_0_rgba(0,0,0,0.04)] md:px-12'>
            <p className='text-xl font-semibold tracking-[-0.02em] text-[#6e6e73]'>
              实时模型排行
            </p>
            <h2 className='mx-auto mt-2 max-w-2xl text-[clamp(2.6rem,4.7vw,4.9rem)] leading-[0.96] font-semibold tracking-[-0.055em]'>
              热门模型，
              <br />
              一眼看清。
            </h2>
            <div className='mx-auto mt-9 max-w-2xl overflow-hidden rounded-[28px] bg-white text-left shadow-[0_20px_60px_rgba(0,0,0,0.08)]'>
              {visibleModels.slice(0, 5).map((model, index) => (
                <div
                  key={model.name}
                  className='border-b border-[#e8e8ed] px-5 py-4 last:border-b-0'
                >
                  <div className='grid grid-cols-[auto_1fr_auto] items-center gap-4'>
                    <div className='flex size-9 items-center justify-center rounded-full bg-[#f5f5f7] text-sm font-semibold text-[#6e6e73]'>
                      {model.rank || index + 1}
                    </div>
                    <div className='min-w-0'>
                      <div className='truncate text-base font-semibold'>
                        {model.name}
                      </div>
                      <div className='mt-1 truncate text-sm font-medium text-[#86868b]'>
                        {model.vendor} · {model.endpoint}
                      </div>
                    </div>
                    <div className='text-right text-sm font-semibold text-[#1d1d1f] tabular-nums'>
                      {model.callsLabel}
                    </div>
                  </div>
                  <div className='mt-3 h-1.5 overflow-hidden rounded-full bg-[#f5f5f7]'>
                    <div
                      className='h-full rounded-full bg-[#0071e3]'
                      style={{ width: getShareWidth(model.share) }}
                    />
                  </div>
                </div>
              ))}
            </div>
          </article>
        </div>
      </section>

      <section className='px-4 py-4 md:px-6'>
        <div className='mx-auto grid max-w-[1400px] gap-4 md:grid-cols-2 lg:grid-cols-4'>
          {features.map((feature) => (
            <article
              key={feature.title}
              className='min-h-[300px] rounded-[30px] bg-white p-7 shadow-[0_1px_0_rgba(0,0,0,0.04)]'
            >
              <div className='mb-8 flex size-12 items-center justify-center rounded-2xl bg-[#f5f5f7] text-[#0071e3]'>
                {feature.icon}
              </div>
              <h3 className='text-3xl leading-tight font-semibold tracking-[-0.04em]'>
                {feature.title}
              </h3>
              <p className='mt-4 text-base leading-7 font-medium text-[#6e6e73]'>
                {feature.desc}
              </p>
            </article>
          ))}
        </div>
      </section>

      <section className='px-4 pt-4 pb-16 md:px-6 md:pb-24'>
        <div className='mx-auto max-w-[1400px] overflow-hidden rounded-[34px] bg-white shadow-[0_1px_0_rgba(0,0,0,0.04)]'>
          <div className='grid lg:grid-cols-[0.82fr_1.18fr]'>
            <div className='flex flex-col justify-center px-7 py-12 md:px-12'>
              <p className='text-xl font-semibold tracking-[-0.02em] text-[#6e6e73]'>
                真实供应商目录
              </p>
              <h2 className='mt-3 text-[clamp(2.6rem,5vw,5rem)] leading-[0.96] font-semibold tracking-[-0.055em]'>
                数据来自
                <br />
                Kalos 当前站点。
              </h2>
              <p className='mt-6 max-w-xl text-xl leading-8 font-medium text-[#6e6e73]'>
                模型、供应商、分组和接口统计会随公开价格与排行接口自动更新。
              </p>
              <Button
                className='mt-8 h-11 w-fit rounded-full bg-[#0071e3] px-6 text-base font-medium text-white hover:bg-[#0077ed]'
                render={<Link to='/pricing' />}
              >
                查看完整价格
                <ArrowUpRight className='ml-2 size-4' />
              </Button>
            </div>
            <div className='divide-y divide-[#e8e8ed] border-t border-[#e8e8ed] lg:border-t-0 lg:border-l'>
              {(vendorRows.length > 0 ? vendorRows : [])
                .slice(0, 8)
                .map((vendor) => (
                  <div
                    key={vendor.name}
                    className='grid grid-cols-[1fr_auto] gap-5 px-6 py-5 md:px-8'
                  >
                    <div className='min-w-0'>
                      <div className='truncate text-base font-semibold'>
                        {vendor.name}
                      </div>
                      <div className='mt-1 truncate text-sm font-medium text-[#86868b]'>
                        {vendor.endpoints}
                      </div>
                    </div>
                    <div className='self-center rounded-full bg-[#f5f5f7] px-3 py-1 text-sm font-semibold text-[#6e6e73]'>
                      {vendor.modelCount}
                    </div>
                  </div>
                ))}
              {vendorRows.length === 0 && (
                <div className='px-6 py-8 text-sm font-medium text-[#86868b] md:px-8'>
                  正在同步供应商目录...
                </div>
              )}
            </div>
          </div>
        </div>
      </section>
    </main>
  )
}

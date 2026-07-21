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
import { useQuery } from '@tanstack/react-query'
import { BadgeCheck, Gauge, LockKeyhole, Sparkles } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Markdown } from '@/components/ui/markdown'
import { Skeleton } from '@/components/ui/skeleton'

import { getAboutContent } from './api'

function isValidUrl(value: string) {
  try {
    const url = new URL(value)
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

function isLikelyHtml(value: string) {
  return /<\/?[a-z][\s\S]*>/i.test(value)
}

function EmptyAboutState() {
  return (
    <div className='kalos-about-page mx-auto flex min-h-[68vh] max-w-6xl items-center px-4 py-16'>
      <section className='w-full overflow-hidden rounded-3xl border border-black/8 bg-white/75 shadow-[0_1px_2px_rgb(0_0_0/0.035),0_24px_80px_rgb(0_0_0/0.05)] backdrop-blur-xl'>
        <div className='grid gap-0 lg:grid-cols-[1.1fr_0.9fr]'>
          <div className='space-y-8 p-8 sm:p-10 lg:p-12'>
            <div className='inline-flex items-center gap-2 rounded-full border border-black/8 bg-zinc-100/80 px-3 py-1 text-xs font-medium text-zinc-600'>
              <Sparkles className='size-3.5' />
              Kalos AI 服务平台
            </div>

            <div className='space-y-4'>
              <h1 className='max-w-3xl text-4xl leading-[1.05] font-semibold tracking-normal text-zinc-950 sm:text-5xl lg:text-6xl'>
                稳定、清晰、面向真实调用的 AI 接入服务
              </h1>
              <p className='max-w-2xl text-base leading-8 text-zinc-600'>
                Kalos
                提供统一的模型访问、用量统计、密钥管理与计费能力，帮助用户以更简单的方式接入多种
                AI 模型，并在控制台中清楚掌握调用、消耗与服务状态。
              </p>
            </div>

            <div className='grid gap-3 sm:grid-cols-3'>
              <AboutMetric label='统一接入' value='多模型' />
              <AboutMetric label='数据视图' value='实时统计' />
              <AboutMetric label='服务管理' value='可配置' />
            </div>
          </div>

          <div className='border-t border-black/6 bg-zinc-100/70 p-6 sm:p-8 lg:border-t-0 lg:border-l'>
            <div className='grid h-full content-center gap-3'>
              <AboutFeature
                icon={<Gauge className='size-4' />}
                title='调用与消耗可视化'
                description='查看调用趋势、模型排行、Token 消耗与请求状态。'
              />
              <AboutFeature
                icon={<LockKeyhole className='size-4' />}
                title='密钥与权限管理'
                description='通过控制台管理 API 密钥、分组、额度和访问策略。'
              />
              <AboutFeature
                icon={<BadgeCheck className='size-4' />}
                title='后台内容可配置'
                description='管理员填写关于页 HTML 或 URL 后，这里会自动替换为后台配置内容。'
              />
            </div>
          </div>
        </div>

        <div className='border-t border-black/6 px-8 py-4 text-center text-xs text-zinc-500 sm:px-10 lg:px-12'>
          本页面为 Kalos 默认展示内容。开源许可信息请查看{' '}
          <a
            href='https://github.com/QuantumNous/new-api/blob/main/LICENSE'
            target='_blank'
            rel='noopener noreferrer'
            className='font-medium text-zinc-700 underline-offset-4 hover:underline'
          >
            AGPL v3.0 License
          </a>
          。
        </div>
      </section>
    </div>
  )
}

function AboutMetric(props: { label: string; value: string }) {
  return (
    <div className='rounded-2xl border border-black/7 bg-zinc-50/80 px-4 py-3'>
      <div className='text-xs text-zinc-500'>{props.label}</div>
      <div className='mt-1 text-lg font-semibold text-zinc-950'>
        {props.value}
      </div>
    </div>
  )
}

function AboutFeature(props: {
  icon: React.ReactNode
  title: string
  description: string
}) {
  return (
    <div className='rounded-2xl border border-black/7 bg-white/70 p-4'>
      <div className='flex items-start gap-3'>
        <div className='grid size-8 shrink-0 place-items-center rounded-full bg-zinc-900 text-white'>
          {props.icon}
        </div>
        <div className='min-w-0'>
          <h2 className='text-sm font-semibold text-zinc-950'>{props.title}</h2>
          <p className='mt-1 text-sm leading-6 text-zinc-600'>
            {props.description}
          </p>
        </div>
      </div>
    </div>
  )
}

export function About() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['about-content'],
    queryFn: getAboutContent,
  })

  const rawContent = data?.data?.trim() ?? ''
  const hasContent = rawContent.length > 0
  const isUrl = hasContent && isValidUrl(rawContent)
  const isHtml = hasContent && !isUrl && isLikelyHtml(rawContent)

  if (isLoading) {
    return (
      <PublicLayout>
        <div className='mx-auto flex max-w-4xl flex-col gap-4 py-12'>
          <Skeleton className='h-8 w-[45%]' />
          <Skeleton className='h-4 w-full' />
          <Skeleton className='h-4 w-[90%]' />
          <Skeleton className='h-4 w-[80%]' />
        </div>
      </PublicLayout>
    )
  }

  if (!hasContent) {
    return (
      <PublicLayout>
        <EmptyAboutState />
      </PublicLayout>
    )
  }

  if (isUrl) {
    return (
      <PublicLayout showMainContainer={false}>
        <iframe
          src={rawContent}
          className='h-[calc(100vh-3.5rem)] w-full border-0'
          title={t('About')}
        />
      </PublicLayout>
    )
  }

  return (
    <PublicLayout>
      <div className='mx-auto max-w-6xl px-4 py-8'>
        {isHtml ? (
          <div
            className='prose prose-neutral dark:prose-invert max-w-none'
            dangerouslySetInnerHTML={{ __html: rawContent }}
          />
        ) : (
          <Markdown className='prose-neutral dark:prose-invert max-w-none'>
            {rawContent}
          </Markdown>
        )}
      </div>
    </PublicLayout>
  )
}

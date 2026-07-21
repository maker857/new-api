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
import {
  BarChart3,
  Gauge,
  GitBranch,
  KeyRound,
  Layers3,
  ReceiptText,
  ShieldCheck,
  SlidersHorizontal,
} from 'lucide-react'

import { AnimateInView } from '@/components/animate-in-view'

export function Features() {
  const features = [
    {
      icon: <KeyRound className='size-4' />,
      title: '统一令牌',
      desc: '为不同应用创建独立 Key，控制额度、过期时间和可用模型。',
    },
    {
      icon: <GitBranch className='size-4' />,
      title: '渠道路由',
      desc: '按模型、分组和上游渠道调度请求，降低单一供应商依赖。',
    },
    {
      icon: <ReceiptText className='size-4' />,
      title: '透明计费',
      desc: '价格页直接展示模型价格信息、固定价格与可用分组。',
    },
    {
      icon: <BarChart3 className='size-4' />,
      title: '请求日志',
      desc: '查看用户、模型、渠道、耗时、用量和成本，方便排查问题。',
    },
    {
      icon: <Gauge className='size-4' />,
      title: '额度与限速',
      desc: '对用户和令牌做额度控制，保护上游账号稳定运行。',
    },
    {
      icon: <ShieldCheck className='size-4' />,
      title: '权限隔离',
      desc: '管理员、普通用户、令牌权限分层管理，适合团队和客户使用。',
    },
    {
      icon: <Layers3 className='size-4' />,
      title: '多协议兼容',
      desc: '支持 OpenAI、Anthropic、Gemini、图像生成等公开端点。',
    },
    {
      icon: <SlidersHorizontal className='size-4' />,
      title: '站点配置',
      desc: '注册、公告、充值、绘图、任务、数据导出等能力可按需开启。',
    },
  ]

  return (
    <section className='px-4 py-16 md:px-6 md:py-24'>
      <div className='mx-auto max-w-7xl'>
        <AnimateInView className='mb-10 max-w-3xl'>
          <p className='text-muted-foreground mb-3 text-xs font-medium tracking-[0.18em] uppercase'>
            接入与运营能力
          </p>
          <h2 className='text-3xl leading-tight font-bold tracking-normal md:text-4xl'>
            从令牌、路由到日志与权限，把模型调用管理成稳定的产品能力。
          </h2>
        </AnimateInView>

        <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-4'>
          {features.map((feature, index) => (
            <AnimateInView
              key={feature.title}
              delay={index * 45}
              className='border-border/80 bg-background hover:bg-muted/25 flex min-h-[154px] flex-col rounded-lg border p-5 shadow-sm transition-colors'
            >
              <span className='border-border/70 bg-muted/30 text-muted-foreground mb-5 flex size-9 items-center justify-center rounded-md border'>
                {feature.icon}
              </span>
              <h3 className='text-sm font-semibold'>{feature.title}</h3>
              <p className='text-muted-foreground mt-3 text-sm leading-6'>
                {feature.desc}
              </p>
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}

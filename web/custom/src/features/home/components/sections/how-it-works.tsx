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
import { Activity, ArrowRight, KeyRound, TerminalSquare } from 'lucide-react'

import { AnimateInView } from '@/components/animate-in-view'
import { Button } from '@/components/ui/button'
import { KALOS_PUBLIC_ORIGIN } from '@/features/home/hooks'

export function HowItWorks() {
  const steps = [
    {
      icon: <KeyRound className='size-5' />,
      title: '创建令牌',
      desc: '在控制台生成 API Key，为不同应用配置额度、过期时间和可用模型。',
      code: 'Authorization: Bearer sk-...',
    },
    {
      icon: <TerminalSquare className='size-5' />,
      title: '替换 Base URL',
      desc: '保留 OpenAI SDK 的调用方式，只需要把接口地址切换到 Kalos。',
      code: `base_url = "${KALOS_PUBLIC_ORIGIN}/v1"`,
    },
    {
      icon: <Activity className='size-5' />,
      title: '查看用量与日志',
      desc: '每次请求的模型、渠道、耗时、额度消耗都可以在控制台追踪。',
      code: '日志 -> 用户 -> 模型 -> 渠道 -> 费用',
    },
  ]

  return (
    <section className='border-border/70 bg-muted/15 border-y px-4 py-16 md:px-6 md:py-24'>
      <div className='mx-auto max-w-7xl'>
        <div className='grid gap-10 lg:grid-cols-[0.8fr_1.2fr] lg:items-start'>
          <AnimateInView>
            <p className='text-muted-foreground mb-3 text-xs font-medium tracking-[0.18em] uppercase'>
              接入流程
            </p>
            <h2 className='text-3xl leading-tight font-bold tracking-normal md:text-4xl'>
              不换 SDK，也能接入多家模型。
            </h2>
            <p className='text-muted-foreground mt-5 max-w-xl text-sm leading-7 md:text-base'>
              对现有应用来说，Kalos
              更像一个统一的模型网关：上层保持熟悉的请求结构，下层由站点处理路由、计费、日志和权限。
            </p>
            <Button
              variant='outline'
              className='border-border/70 bg-background mt-7 rounded-md'
              render={<Link to='/pricing' />}
            >
              查看真实价格表
              <ArrowRight className='ml-2 size-4' />
            </Button>
          </AnimateInView>

          <div className='grid gap-3'>
            {steps.map((step, index) => (
              <AnimateInView
                key={step.title}
                delay={index * 90}
                className='border-border/80 bg-background grid gap-4 rounded-lg border p-4 shadow-sm md:grid-cols-[220px_1fr]'
              >
                <div className='flex items-start gap-3'>
                  <span className='border-border/70 bg-muted/30 text-muted-foreground flex size-10 items-center justify-center rounded-md border'>
                    {step.icon}
                  </span>
                  <div>
                    <div className='text-muted-foreground text-xs'>
                      0{index + 1}
                    </div>
                    <h3 className='font-semibold'>{step.title}</h3>
                  </div>
                </div>
                <div>
                  <p className='text-muted-foreground text-sm leading-6'>
                    {step.desc}
                  </p>
                  <code className='border-border/70 bg-muted/25 text-foreground mt-3 block overflow-x-auto rounded-md border px-3 py-2 font-mono text-xs'>
                    {step.code}
                  </code>
                </div>
              </AnimateInView>
            ))}
          </div>
        </div>
      </div>
    </section>
  )
}

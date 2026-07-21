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
import { ArrowRight, BookOpen, CreditCard, LayoutDashboard } from 'lucide-react'

import { AnimateInView } from '@/components/animate-in-view'
import { Button } from '@/components/ui/button'
import { KALOS_PUBLIC_ORIGIN } from '@/features/home/hooks'
import { useStatus } from '@/hooks/use-status'

interface CTAProps {
  className?: string
  isAuthenticated?: boolean
}

export function CTA(props: CTAProps) {
  const { status } = useStatus()
  const registerEnabled = status?.register_enabled !== false

  const accountTarget: '/dashboard' | '/sign-up' | '/sign-in' = props.isAuthenticated
    ? '/dashboard'
    : registerEnabled
      ? '/sign-up'
      : '/sign-in'

  const actions = [
    {
      icon: <LayoutDashboard className='size-4' />,
      title: props.isAuthenticated
        ? '进入控制台'
        : registerEnabled
          ? '注册账号'
          : '登录账号',
      desc: props.isAuthenticated
        ? '管理令牌、渠道、日志、额度和充值记录。'
        : registerEnabled
          ? '创建账号后即可生成令牌并开始调用模型。'
          : '当前站点未开放注册，请使用已有账号登录。',
      to: accountTarget,
    },
    {
      icon: <CreditCard className='size-4' />,
      title: '模型价格',
      desc: '查看 Kalos 当前公开的模型价格、固定价格和可用分组。',
      to: '/pricing',
    },
  ]

  return (
    <section className='px-4 py-16 md:px-6 md:py-24'>
      <AnimateInView className='border-border/80 bg-background mx-auto max-w-7xl rounded-lg border p-6 shadow-sm md:p-8'>
        <div className='grid gap-8 lg:grid-cols-[1fr_auto] lg:items-center'>
          <div>
            <p className='text-muted-foreground mb-3 text-xs font-medium tracking-[0.18em] uppercase'>
              开始使用
            </p>
            <h2 className='text-2xl leading-tight font-bold tracking-normal md:text-4xl'>
              用 Kalos 做你的统一 AI 模型入口。
            </h2>
          </div>
          <div className='flex flex-wrap gap-3'>
            <Button className='rounded-md' render={<Link to={accountTarget} />}>
              {props.isAuthenticated
                ? '控制台'
                : registerEnabled
                  ? '立即注册'
                  : '立即登录'}
              <ArrowRight className='ml-2 size-4' />
            </Button>
            <Button
              variant='outline'
              className='border-border/70 bg-background rounded-md'
              render={
                <a
                  href={`${KALOS_PUBLIC_ORIGIN}/pricing`}
                  target='_blank'
                  rel='noopener noreferrer'
                />
              }
            >
              <BookOpen className='mr-2 size-4' />
              模型广场
            </Button>
          </div>
        </div>

        <div className='mt-8 grid gap-3 md:grid-cols-2'>
          {actions.map((action) => (
            <Link
              key={action.title}
              to={action.to}
              className='border-border/70 bg-muted/20 hover:bg-muted/35 rounded-lg border p-4 transition-colors'
            >
              <div className='mb-2 flex items-center gap-2 font-medium'>
                <span className='border-border/70 bg-background text-muted-foreground flex size-8 items-center justify-center rounded-md border'>
                  {action.icon}
                </span>
                {action.title}
              </div>
              <p className='text-muted-foreground text-sm leading-6'>
                {action.desc}
              </p>
            </Link>
          ))}
        </div>
      </AnimateInView>
    </section>
  )
}

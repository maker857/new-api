import { useNavigate } from '@tanstack/react-router'
import { CheckIcon, CopyIcon } from 'lucide-react'
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
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useCountdown } from '@/hooks/use-countdown'
import { api } from '@/lib/api'
import { copyToClipboard } from '@/lib/copy-to-clipboard'

import { AuthLayout } from '../auth-layout'

export type ResetPasswordSearchParams = {
  email?: string
  token?: string
}

type ResetPasswordConfirmProps = ResetPasswordSearchParams

export function ResetPasswordConfirm({
  email,
  token,
}: ResetPasswordConfirmProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [newPassword, setNewPassword] = useState('')
  const [loading, setLoading] = useState(false)
  const [copied, setCopied] = useState(false)
  const {
    secondsLeft,
    isActive,
    start: startCountdown,
  } = useCountdown({ initialSeconds: 30 })

  const isValidResetLink = Boolean(email && token)

  async function handleSubmit() {
    if (!isValidResetLink || !email || !token) {
      toast.error(t('Invalid reset link, please request a new password reset'))
      return
    }

    startCountdown()
    setLoading(true)
    try {
      const res = await api.post('/api/user/reset', { email, token }, {
        skipBusinessError: true,
      } as Record<string, unknown>)

      if (res?.data?.success) {
        const password = res.data.data
        setNewPassword(password)
        const copySuccess = await copyToClipboard(password)
        if (copySuccess) {
          toast.success(
            t('Password reset and copied to clipboard: {{password}}', {
              password,
            })
          )
        } else {
          toast.success(t('Password reset: {{password}}', { password }))
        }
      }
    } catch {
      // Errors handled by global interceptor
    } finally {
      setLoading(false)
    }
  }

  async function handleCopy() {
    if (!newPassword) return

    const copySuccess = await copyToClipboard(newPassword)
    if (copySuccess) {
      setCopied(true)
      toast.success(
        t('Password copied to clipboard: {{password}}', {
          password: newPassword,
        })
      )
      setTimeout(() => setCopied(false), 2000)
    }
  }

  return (
    <AuthLayout>
      <div className='w-full space-y-8'>
        <div className='space-y-3'>
          <h2 className='text-2xl font-semibold tracking-normal text-neutral-950'>
            重置密码
          </h2>
          <p className='text-sm leading-6 text-neutral-600'>
            {newPassword
              ? '新密码已生成，请妥善保存并返回登录。'
              : '确认邮箱重置链接后，系统会生成一个新的临时密码。'}
          </p>
        </div>

        <div className='space-y-4'>
          {!isValidResetLink && (
            <Alert variant='destructive'>
              <AlertDescription>
                {t('Invalid reset link, please request a new password reset.')}
              </AlertDescription>
            </Alert>
          )}

          <div className='space-y-2'>
            <Label htmlFor='email'>邮箱</Label>
            <Input
              id='email'
              type='email'
              value={email || ''}
              disabled
              placeholder='等待邮箱信息...'
            />
          </div>

          {newPassword && (
            <div className='space-y-2'>
              <Label htmlFor='password'>新密码</Label>
              <div className='flex gap-2'>
                <Input
                  id='password'
                  value={newPassword}
                  disabled
                  className='font-mono'
                />
                <Button
                  type='button'
                  size='icon'
                  variant='outline'
                  onClick={handleCopy}
                  className='shrink-0'
                >
                  {copied ? (
                    <CheckIcon className='h-4 w-4' />
                  ) : (
                    <CopyIcon className='h-4 w-4' />
                  )}
                </Button>
              </div>
              <p className='text-muted-foreground text-xs'>
                密码已复制到剪贴板，请及时保存。
              </p>
            </div>
          )}

          <Button
            className='w-full'
            onClick={
              newPassword
                ? () => navigate({ to: '/sign-in', replace: true })
                : handleSubmit
            }
            disabled={
              newPassword ? false : loading || isActive || !isValidResetLink
            }
          >
            {newPassword
              ? '返回登录'
              : isActive
                ? `请稍候 (${secondsLeft}s)`
                : '确认重置密码'}
          </Button>

          {!newPassword && (
            <Button
              variant='link'
              className='h-auto min-h-0 w-full text-neutral-950 underline underline-offset-4 hover:text-neutral-600'
              onClick={() => navigate({ to: '/sign-in', replace: true })}
            >
              返回登录
            </Button>
          )}
        </div>
      </div>
    </AuthLayout>
  )
}

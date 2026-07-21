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
import { Link, useSearch } from '@tanstack/react-router'

import { useStatus } from '@/hooks/use-status'

import { AuthLayout } from '../auth-layout'
import { TermsFooter } from '../components/terms-footer'
import { UserAuthForm } from './components/user-auth-form'

export function SignIn() {
  const { redirect } = useSearch({ from: '/(auth)/sign-in' })
  const { status } = useStatus()

  return (
    <AuthLayout>
      <div className='w-full space-y-8'>
        <div className='space-y-3'>
          <h2 className='text-2xl font-semibold tracking-normal text-neutral-950'>
            登录 Kalos
          </h2>
          <p className='text-sm leading-6 text-neutral-600'>
            继续访问你的模型调用控制台。
          </p>
          {!status?.self_use_mode_enabled &&
            status?.register_enabled !== false && (
              <p className='text-sm text-neutral-500'>
                还没有账号？{' '}
                <Link
                  to='/sign-up'
                  className='font-medium text-neutral-950 underline underline-offset-4 hover:text-neutral-600'
                >
                  创建账号
                </Link>
                .
              </p>
            )}
        </div>

        <UserAuthForm redirectTo={redirect} />

        <TermsFooter
          variant='sign-in'
          status={status}
          className='text-center'
        />
      </div>
    </AuthLayout>
  )
}

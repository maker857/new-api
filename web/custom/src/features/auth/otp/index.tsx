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

import { AuthLayout } from '../auth-layout'
import { OtpForm } from './components/otp-form'

export function Otp() {
  return (
    <AuthLayout>
      <div className='w-full space-y-8'>
        <div className='space-y-3'>
          <h2 className='text-2xl font-semibold tracking-normal text-neutral-950'>
            安全验证
          </h2>
          <p className='text-sm leading-6 text-neutral-600'>
            输入身份验证器中的 6 位动态验证码，继续进入控制台。
          </p>
          <p className='text-sm text-neutral-500'>
            登录状态已过期？{' '}
            <Link
              to='/sign-in'
              className='font-medium text-neutral-950 underline underline-offset-4 hover:text-neutral-600'
            >
              重新登录
            </Link>
            .
          </p>
        </div>

        <OtpForm />
      </div>
    </AuthLayout>
  )
}

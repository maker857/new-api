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
'use client'

import {
  CheckmarkCircle02Icon,
  InformationCircleIcon,
  Alert02Icon,
  Loading03Icon,
} from '@hugeicons/core-free-icons'
import { HugeiconsIcon } from '@hugeicons/react'
import { Toaster as Sonner, type ToasterProps } from 'sonner'

import { useTheme } from '@/context/theme-provider'

const Toaster = (props: ToasterProps) => {
  const { resolvedTheme } = useTheme()

  return (
    <Sonner
      theme={resolvedTheme}
      className='toaster group'
      icons={{
        success: (
          <HugeiconsIcon
            icon={CheckmarkCircle02Icon}
            strokeWidth={2}
            className='size-4'
          />
        ),
        info: (
          <HugeiconsIcon
            icon={InformationCircleIcon}
            strokeWidth={2}
            className='size-4'
          />
        ),
        warning: (
          <HugeiconsIcon
            icon={Alert02Icon}
            strokeWidth={2}
            className='size-4'
          />
        ),
        error: (
          <HugeiconsIcon
            icon={Alert02Icon}
            strokeWidth={2}
            className='size-4'
          />
        ),
        loading: (
          <HugeiconsIcon
            icon={Loading03Icon}
            strokeWidth={2}
            className='size-4 animate-spin'
          />
        ),
      }}
      style={
        {
          '--normal-bg': 'rgb(244 244 245 / 0.9)',
          '--normal-text': '#1d1d1f',
          '--normal-border': 'rgb(0 0 0 / 0.08)',
          '--success-bg': 'rgb(244 244 245 / 0.9)',
          '--success-border': 'rgb(0 0 0 / 0.08)',
          '--success-text': '#047857',
          '--info-bg': 'rgb(244 244 245 / 0.9)',
          '--info-border': 'rgb(0 0 0 / 0.08)',
          '--info-text': '#2563eb',
          '--warning-bg': 'rgb(244 244 245 / 0.9)',
          '--warning-border': 'rgb(0 0 0 / 0.08)',
          '--warning-text': '#b45309',
          '--error-bg': 'rgb(244 244 245 / 0.9)',
          '--error-border': 'rgb(0 0 0 / 0.08)',
          '--error-text': '#dc2626',
          '--border-radius': '1rem',
        } as React.CSSProperties
      }
      {...props}
    />
  )
}

export { Toaster }

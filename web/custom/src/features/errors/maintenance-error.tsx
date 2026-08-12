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
import { useNavigate, useRouter } from '@tanstack/react-router'
import { Wrench } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { ErrorPageShell } from './error-page-shell'

export function MaintenanceError() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history } = useRouter()
  return (
    <ErrorPageShell
      status={503}
      title={t('KalosAI is being maintained')}
      description={t(
        'The service is temporarily unavailable while we complete maintenance. Please try again later.'
      )}
      icon={<Wrench className='size-6' aria-hidden='true' />}
      onBack={() => history.go(-1)}
      onHome={() => navigate({ to: '/' })}
    />
  )
}

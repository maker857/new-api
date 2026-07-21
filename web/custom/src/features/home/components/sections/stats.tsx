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
import { usePublicSiteData } from '@/features/home/hooks'

export function Stats() {
  const { models, vendors, groups, endpoints } = usePublicSiteData()

  const stats = [
    [`${models.length || '-'}`, '公开模型'],
    [`${vendors.length || '-'}`, '供应商'],
    [`${groups.length || '-'}`, '可用分组'],
    [`${endpoints.length || '-'}`, '兼容接口'],
  ]

  return (
    <section className='border-border/70 border-b px-4 md:px-6'>
      <div className='divide-border/70 mx-auto grid max-w-7xl divide-y md:grid-cols-4 md:divide-x md:divide-y-0'>
        {stats.map(([value, label]) => (
          <div key={label} className='py-6 md:px-6'>
            <div className='text-2xl font-semibold tracking-normal md:text-3xl'>
              {value}
            </div>
            <div className='text-muted-foreground mt-1 text-sm'>{label}</div>
          </div>
        ))}
      </div>
      <div className='text-muted-foreground mx-auto max-w-7xl pb-4 text-xs'>
        数据来自 Kalos 公开价格与排行接口，页面会随模型目录自动更新。
      </div>
    </section>
  )
}

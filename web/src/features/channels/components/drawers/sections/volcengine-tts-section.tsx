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
import { AudioLines } from 'lucide-react'
import { useMemo } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import {
  sideDrawerSwitchItemClassName,
  SideDrawerSection,
  SideDrawerSectionHeader,
} from '@/components/drawer-layout'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'

import type { ChannelFormValues } from '../../../lib/channel-form'
import { VOLC_TTS_RESOURCE_IDS } from '../../../lib/volcengine-tts'

type VolcEngineTTSSectionProps = {
  form: UseFormReturn<ChannelFormValues>
  disabled?: boolean
}

export function VolcEngineTTSSection(props: VolcEngineTTSSectionProps) {
  const { t } = useTranslation()
  const protocol = props.form.watch('volc_tts_protocol') || 'v1_ws_binary'
  const authMode = props.form.watch('volc_tts_auth_mode') || 'new_console'
  const protocolItems = useMemo(
    () => [
      { value: 'v1_ws_binary', label: t('Legacy v1 WebSocket') },
      {
        value: 'v3_ws_uni',
        label: t('v3 unidirectional WebSocket'),
      },
      { value: 'v3_http_chunked', label: t('v3 HTTP Chunked') },
    ],
    [t]
  )
  const authItems = useMemo(
    () => [
      { value: 'new_console', label: t('New console API key') },
      { value: 'legacy', label: t('Legacy app credentials') },
    ],
    [t]
  )

  let protocolDescription = t('Keeps the legacy v1 WebSocket TTS path.')
  if (protocol === 'v3_ws_uni') {
    protocolDescription = t(
      'Streams one complete text request over the v3 unidirectional WebSocket endpoint.'
    )
  } else if (protocol === 'v3_http_chunked') {
    protocolDescription = t(
      'Streams v3 binary audio frames over HTTP Chunked without a total response timeout.'
    )
  }

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader
        title={t('Doubao TTS')}
        description={t(
          'Configure the protocol, billing resource, authentication, and usage reporting for VolcEngine TTS.'
        )}
        icon={<AudioLines className='h-4 w-4' aria-hidden='true' />}
        iconTone='info'
      />

      <fieldset
        disabled={props.disabled}
        className='space-y-4 disabled:opacity-60'
      >
        <FormField
          control={props.form.control}
          name='volc_tts_protocol'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Speech protocol')}</FormLabel>
              <Select
                items={protocolItems}
                value={field.value || 'v1_ws_binary'}
                onValueChange={field.onChange}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {protocolItems.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FormDescription>{protocolDescription}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={props.form.control}
          name='volc_tts_resource_id'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Resource ID')}</FormLabel>
              <FormControl>
                <Input
                  list='volc-tts-resource-ids'
                  placeholder='seed-tts-2.0'
                  {...field}
                />
              </FormControl>
              <datalist id='volc-tts-resource-ids'>
                {VOLC_TTS_RESOURCE_IDS.map((resourceID) => (
                  <option key={resourceID} value={resourceID} />
                ))}
              </datalist>
              <FormDescription>
                {t(
                  'Choose a resource that matches the routed model and voice family.'
                )}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={props.form.control}
          name='volc_tts_auth_mode'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('Authentication mode')}</FormLabel>
              <Select
                items={authItems}
                value={field.value || 'new_console'}
                onValueChange={field.onChange}
              >
                <FormControl>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                </FormControl>
                <SelectContent alignItemWithTrigger={false}>
                  <SelectGroup>
                    {authItems.map((item) => (
                      <SelectItem key={item.value} value={item.value}>
                        {item.label}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FormDescription>
                {authMode === 'legacy'
                  ? t(
                      'Use appid|access_token; it is mapped to X-Api-App-Id and X-Api-Access-Key.'
                    )
                  : t('Use a single API key; it is sent as X-Api-Key.')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={props.form.control}
          name='volc_tts_require_usage'
          render={({ field }) => (
            <FormItem className={sideDrawerSwitchItemClassName()}>
              <div className='flex flex-col gap-0.5'>
                <FormLabel>{t('Return upstream usage')}</FormLabel>
                <FormDescription className='text-xs'>
                  {t(
                    'Request text_words usage from VolcEngine for billing; character estimates remain the fallback.'
                  )}
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value !== false}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />
      </fieldset>
    </SideDrawerSection>
  )
}

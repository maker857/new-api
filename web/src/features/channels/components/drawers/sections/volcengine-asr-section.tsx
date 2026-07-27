import { Mic2 } from 'lucide-react'
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

const VOLC_ASR_RESOURCE_IDS = ['volc.bigasr.auc', 'volc.seedasr.auc'] as const

type VolcEngineASRSectionProps = {
  form: UseFormReturn<ChannelFormValues>
  disabled?: boolean
}

export function VolcEngineASRSection(props: VolcEngineASRSectionProps) {
  const { t } = useTranslation()
  const enabled = props.form.watch('volc_asr_enabled') === true
  const authMode = props.form.watch('volc_asr_auth_mode') || 'new_console'
  const authItems = useMemo(
    () => [
      { value: 'new_console', label: t('New console API key') },
      { value: 'legacy', label: t('Legacy app credentials') },
    ],
    [t]
  )

  return (
    <SideDrawerSection>
      <SideDrawerSectionHeader
        title={t('Doubao ASR')}
        description={t(
          'Configure native VolcEngine ASR v3 with word-level timestamps.'
        )}
        icon={<Mic2 className='h-4 w-4' aria-hidden='true' />}
        iconTone='info'
      />

      <fieldset
        disabled={props.disabled}
        className='space-y-4 disabled:opacity-60'
      >
        <FormField
          control={props.form.control}
          name='volc_asr_enabled'
          render={({ field }) => (
            <FormItem className={sideDrawerSwitchItemClassName()}>
              <div className='flex flex-col gap-0.5'>
                <FormLabel>{t('Enable native ASR')}</FormLabel>
                <FormDescription className='text-xs'>
                  {t('Use this VolcEngine channel for native ASR v3 requests.')}
                </FormDescription>
              </div>
              <FormControl>
                <Switch
                  checked={field.value === true}
                  onCheckedChange={field.onChange}
                />
              </FormControl>
            </FormItem>
          )}
        />

        <FormField
          control={props.form.control}
          name='volc_asr_resource_id'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('ASR Resource ID')}</FormLabel>
              <FormControl>
                <Input
                  list='volc-asr-resource-ids'
                  placeholder='volc.seedasr.auc'
                  disabled={!enabled}
                  {...field}
                />
              </FormControl>
              <datalist id='volc-asr-resource-ids'>
                {VOLC_ASR_RESOURCE_IDS.map((resourceID) => (
                  <option key={resourceID} value={resourceID} />
                ))}
              </datalist>
              <FormDescription>
                {t('Use the ASR resource ID issued by the VolcEngine console.')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />

        <FormField
          control={props.form.control}
          name='volc_asr_auth_mode'
          render={({ field }) => (
            <FormItem>
              <FormLabel>{t('ASR Authentication mode')}</FormLabel>
              <Select
                items={authItems}
                value={field.value || 'new_console'}
                onValueChange={field.onChange}
                disabled={!enabled}
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
                  ? t('Use appid|access_token for the legacy ASR credential.')
                  : t('Use a single API key for the new ASR console.')}
              </FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
      </fieldset>
    </SideDrawerSection>
  )
}

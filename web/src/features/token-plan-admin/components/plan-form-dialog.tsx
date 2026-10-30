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
import { useQuery } from '@tanstack/react-query'
import { Wand2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import {
  formatTokens,
  formatWindowDuration,
} from '@/features/token-plan/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import {
  createTokenPlan,
  getTokenPlanGroupModels,
  updateTokenPlan,
} from '../api'
import {
  emptyTokenPlan,
  type TokenPlanAdminRecord,
  type TokenPlanMeteringMode,
} from '../types'

const GROUP_MODELS_QUERY_KEY = ['token-plan-admin', 'group-models']

/** 逗号分隔列表 -> 去空切片。 */
function splitList(raw: string): string[] {
  return raw
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

/** 去重后写回逗号分隔列表。 */
function joinList(items: string[]): string {
  return [...new Set(items)].join(',')
}

type PlanFormDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  initial?: TokenPlanAdminRecord | null
  onSaved: () => void
}

export function PlanFormDialog({
  open,
  onOpenChange,
  initial,
  onSaved,
}: PlanFormDialogProps) {
  const { t } = useTranslation()
  const [form, setForm] = useState<TokenPlanAdminRecord>(emptyTokenPlan)
  const [saving, setSaving] = useState(false)

  const groupModelsQuery = useQuery({
    queryKey: GROUP_MODELS_QUERY_KEY,
    queryFn: async () => {
      const result = await getTokenPlanGroupModels()
      if (!result.success) {
        throw createServerError(result, t('Failed to load groups'))
      }
      return result.data ?? {}
    },
    enabled: open,
    staleTime: 60_000,
  })

  useEffect(() => {
    if (!open) return
    setForm(initial ? { ...initial } : emptyTokenPlan())
  }, [open, initial])

  const set = <K extends keyof TokenPlanAdminRecord>(
    key: K,
    value: TokenPlanAdminRecord[K]
  ) => setForm((prev) => ({ ...prev, [key]: value }))

  const isEdit = form.id > 0
  const groupModels = groupModelsQuery.data ?? {}
  const groupNames = Object.keys(groupModels).sort()
  const selectedGroups = splitList(form.group_limits)

  const toggleGroup = (group: string) => {
    const next = selectedGroups.includes(group)
      ? selectedGroups.filter((item) => item !== group)
      : [...selectedGroups, group]
    set('group_limits', next.join(','))
  }

  // 把选中分组下已启用的模型并入「可用模型」，保留管理员手工加过的条目。
  const applyGroupModels = () => {
    if (!selectedGroups.length) {
      toast.error(t('Select at least one group first'))
      return
    }
    const picked = selectedGroups.flatMap((group) => groupModels[group] ?? [])
    if (!picked.length) {
      toast.error(t('The selected groups have no enabled models'))
      return
    }
    const merged = joinList([...splitList(form.model_limits), ...picked])
    set('model_limits', merged)
    toast.success(
      t('Filled {{count}} models from the selected groups', {
        count: picked.length,
      })
    )
  }

  const groupsSection = (() => {
    if (groupModelsQuery.isLoading) {
      return (
        <p className='text-muted-foreground text-[11px]'>
          {t('Loading groups...')}
        </p>
      )
    }
    if (groupNames.length === 0) {
      return (
        <p className='text-muted-foreground text-[11px]'>
          {t('No groups available')}
        </p>
      )
    }
    return (
      <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
        {groupNames.map((group) => (
          <label
            key={group}
            htmlFor={`tp-group-${group}`}
            className='hover:bg-muted/50 flex cursor-pointer items-center gap-2 rounded-md border px-2.5 py-1.5 text-xs'
          >
            <Checkbox
              id={`tp-group-${group}`}
              checked={selectedGroups.includes(group)}
              onCheckedChange={() => toggleGroup(group)}
            />
            <span className='min-w-0 flex-1 truncate font-medium'>{group}</span>
            <span className='text-muted-foreground tabular-nums'>
              {(groupModels[group] ?? []).length}
            </span>
          </label>
        ))}
      </div>
    )
  })()

  const handleSave = async () => {
    if (!form.title.trim()) {
      toast.error(t('Plan title cannot be empty'))
      return
    }
    setSaving(true)
    try {
      const result = isEdit
        ? await updateTokenPlan(form)
        : await createTokenPlan(form)
      if (!result.success) {
        throw createServerError(result, t('Failed to save the plan'))
      }
      toast.success(t('Plan saved'))
      onOpenChange(false)
      onSaved()
    } catch (error) {
      handleServerError(error, t('Failed to save the plan'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={isEdit ? t('Edit plan') : t('Create plan')}
      description={t(
        'Both windows are anchored to the first request of the plan and reset every N minutes.'
      )}
      contentClassName='sm:max-w-3xl'
      footer={
        <div className='flex justify-end gap-2'>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          <Button disabled={saving} onClick={handleSave}>
            {t('Save')}
          </Button>
        </div>
      }
    >
      <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
        <div className='flex items-center justify-between gap-3 rounded-md border px-3 py-2 sm:col-span-2'>
          <div className='flex flex-col gap-0.5'>
            <Label htmlFor='tp-enabled'>{t('Enabled')}</Label>
            <p className='text-muted-foreground text-[11px]'>
              {t(
                'Disabled plans stay visible to existing subscribers but cannot be purchased.'
              )}
            </p>
          </div>
          <Switch
            id='tp-enabled'
            checked={form.enabled}
            onCheckedChange={(checked) => set('enabled', checked)}
          />
        </div>

        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='tp-title'>{t('Title')}</Label>
          <Input
            id='tp-title'
            value={form.title}
            onChange={(e) => set('title', e.target.value)}
            placeholder='PLUS'
          />
        </div>
        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='tp-subtitle'>{t('Subtitle')}</Label>
          <Input
            id='tp-subtitle'
            value={form.subtitle}
            onChange={(e) => set('subtitle', e.target.value)}
          />
        </div>

        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='tp-price'>{t('Price')}</Label>
          <Input
            id='tp-price'
            type='number'
            value={form.price_amount}
            onChange={(e) => set('price_amount', Number(e.target.value))}
          />
        </div>
        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='tp-duration'>{t('Duration (days)')}</Label>
          <Input
            id='tp-duration'
            type='number'
            value={form.duration_days}
            onChange={(e) => set('duration_days', Number(e.target.value))}
          />
        </div>

        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='tp-total'>{t('Total token quota')}</Label>
          <Input
            id='tp-total'
            type='number'
            value={form.total_tokens}
            onChange={(e) => set('total_tokens', Number(e.target.value))}
          />
        </div>
        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='tp-metering'>{t('Metering mode')}</Label>
          <NativeSelect
            id='tp-metering'
            className='w-full'
            value={form.metering_mode}
            onChange={(e) =>
              set('metering_mode', e.target.value as TokenPlanMeteringMode)
            }
          >
            <NativeSelectOption value='weighted'>
              {t('Weighted by model ratio')}
            </NativeSelectOption>
            <NativeSelectOption value='raw'>
              {t('Raw token count')}
            </NativeSelectOption>
          </NativeSelect>
        </div>

        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='tp-w1-minutes'>
            {t('Window 1 duration (minutes)')}
          </Label>
          <Input
            id='tp-w1-minutes'
            type='number'
            value={form.window1_minutes}
            onChange={(e) => set('window1_minutes', Number(e.target.value))}
          />
          <p className='text-muted-foreground text-[11px]'>
            = {formatWindowDuration(form.window1_minutes * 60)}
          </p>
        </div>
        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='tp-w1-tokens'>{t('Window 1 token quota')}</Label>
          <Input
            id='tp-w1-tokens'
            type='number'
            value={form.window1_tokens}
            onChange={(e) => set('window1_tokens', Number(e.target.value))}
          />
        </div>

        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='tp-w2-minutes'>
            {t('Window 2 duration (minutes)')}
          </Label>
          <Input
            id='tp-w2-minutes'
            type='number'
            value={form.window2_minutes}
            onChange={(e) => set('window2_minutes', Number(e.target.value))}
          />
          <p className='text-muted-foreground text-[11px]'>
            = {formatWindowDuration(form.window2_minutes * 60)}
          </p>
        </div>
        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='tp-w2-tokens'>{t('Window 2 token quota')}</Label>
          <Input
            id='tp-w2-tokens'
            type='number'
            value={form.window2_tokens}
            onChange={(e) => set('window2_tokens', Number(e.target.value))}
          />
        </div>

        <div className='flex flex-col gap-2 sm:col-span-2'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <Label>{t('Available groups')}</Label>
            <Button
              type='button'
              variant='outline'
              size='xs'
              disabled={!selectedGroups.length || groupModelsQuery.isLoading}
              onClick={applyGroupModels}
            >
              <Wand2 className='h-3.5 w-3.5' />
              {t('Fill models from groups')}
            </Button>
          </div>
          {groupsSection}
          <p className='text-muted-foreground text-[11px]'>
            {t(
              'Leave empty to allow every group. Requests from other groups are rejected.'
            )}
          </p>
        </div>

        <div className='flex flex-col gap-1.5 sm:col-span-2'>
          <Label htmlFor='tp-models'>{t('Available models')}</Label>
          <Input
            id='tp-models'
            value={form.model_limits}
            onChange={(e) => set('model_limits', e.target.value)}
            placeholder='gpt-4o, claude-sonnet-4'
          />
          <p className='text-muted-foreground text-[11px]'>
            {t('Comma separated. Leave empty to allow every enabled model.')}
          </p>
        </div>

        <div className='mt-1 flex flex-col gap-1 border-t pt-3 sm:col-span-2'>
          <p className='text-sm font-medium'>{t('Throttling')}</p>
          <p className='text-muted-foreground text-[11px]'>
            {t(
              'Applied per subscription. 0 means unlimited. The delay only slows the first byte, not the total throughput.'
            )}
          </p>
        </div>

        <div className='flex flex-col gap-3 rounded-md border p-3 sm:col-span-2'>
          <div className='flex items-center gap-2'>
            <Switch
              id='tp-fbd-enabled'
              checked={form.first_byte_delay_enabled}
              onCheckedChange={(checked) =>
                set('first_byte_delay_enabled', checked)
              }
            />
            <Label htmlFor='tp-fbd-enabled'>{t('First-byte delay')}</Label>
          </div>

          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
            <div className='flex flex-col gap-1.5'>
              <Label htmlFor='tp-fbd-threshold'>
                {t('Prompt token threshold (0 = always)')}
              </Label>
              <Input
                id='tp-fbd-threshold'
                type='number'
                min={0}
                disabled={!form.first_byte_delay_enabled}
                value={form.first_byte_delay_prompt_threshold}
                onChange={(e) =>
                  set('first_byte_delay_prompt_threshold', Number(e.target.value))
                }
              />
              <p className='text-muted-foreground text-[11px]'>
                {form.first_byte_delay_prompt_threshold > 0
                  ? t('= {{count}} tokens', {
                      count: formatTokens(form.first_byte_delay_prompt_threshold),
                    })
                  : t(
                      '0 means every request is eligible. Enter a plain number, e.g. 500000 (not 500K).'
                    )}
              </p>
            </div>
            <div className='flex flex-col gap-1.5'>
              <Label htmlFor='tp-fbd-probability'>
                {t('Trigger probability (%)')}
              </Label>
              <Input
                id='tp-fbd-probability'
                type='number'
                min={0}
                max={100}
                disabled={!form.first_byte_delay_enabled}
                value={form.first_byte_delay_probability}
                onChange={(e) =>
                  set('first_byte_delay_probability', Number(e.target.value))
                }
              />
            </div>
          </div>

          <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
            <div className='flex flex-col gap-1.5'>
              <Label htmlFor='tp-fbd-min'>{t('Delay min (ms)')}</Label>
              <Input
                id='tp-fbd-min'
                type='number'
                disabled={!form.first_byte_delay_enabled}
                value={form.first_byte_delay_min_ms}
                onChange={(e) =>
                  set('first_byte_delay_min_ms', Number(e.target.value))
                }
              />
            </div>
            <div className='flex flex-col gap-1.5'>
              <Label htmlFor='tp-fbd-max'>{t('Delay max (ms)')}</Label>
              <Input
                id='tp-fbd-max'
                type='number'
                disabled={!form.first_byte_delay_enabled}
                value={form.first_byte_delay_max_ms}
                onChange={(e) =>
                  set('first_byte_delay_max_ms', Number(e.target.value))
                }
              />
            </div>
          </div>
        </div>

        <div className='grid grid-cols-1 gap-3 sm:col-span-2 sm:grid-cols-3'>
          <div className='flex flex-col gap-1.5'>
            <Label htmlFor='tp-concurrency'>
              {t('Concurrency limit (0 = unlimited)')}
            </Label>
            <Input
              id='tp-concurrency'
              type='number'
              value={form.concurrency_limit}
              onChange={(e) =>
                set('concurrency_limit', Number(e.target.value))
              }
            />
          </div>
          <div className='flex flex-col gap-1.5'>
            <Label htmlFor='tp-rate-count'>
              {t('Rate limit requests (0 = unlimited)')}
            </Label>
            <Input
              id='tp-rate-count'
              type='number'
              value={form.rate_limit_count}
              onChange={(e) => set('rate_limit_count', Number(e.target.value))}
            />
          </div>
          <div className='flex flex-col gap-1.5'>
            <Label htmlFor='tp-rate-window'>
              {t('Rate limit window (seconds)')}
            </Label>
            <Input
              id='tp-rate-window'
              type='number'
              disabled={form.rate_limit_count <= 0}
              value={form.rate_limit_window_seconds}
              onChange={(e) =>
                set('rate_limit_window_seconds', Number(e.target.value))
              }
            />
          </div>
        </div>
      </div>
    </Dialog>
  )
}

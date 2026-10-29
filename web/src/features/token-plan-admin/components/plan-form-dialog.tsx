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
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { formatWindowDuration } from '@/features/token-plan/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import { createTokenPlan, updateTokenPlan } from '../api'
import {
  emptyTokenPlan,
  type TokenPlanAdminRecord,
  type TokenPlanMeteringMode,
} from '../types'

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

  useEffect(() => {
    if (!open) return
    setForm(initial ? { ...initial } : emptyTokenPlan())
  }, [open, initial])

  const set = <K extends keyof TokenPlanAdminRecord>(
    key: K,
    value: TokenPlanAdminRecord[K]
  ) => setForm((prev) => ({ ...prev, [key]: value }))

  const isEdit = form.id > 0

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
        'Both windows are anchored to the first request of the plan and reset every N seconds.'
      )}
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

        <div className='flex flex-col gap-1.5 sm:col-span-2'>
          <Label htmlFor='tp-total'>{t('Total token quota')}</Label>
          <Input
            id='tp-total'
            type='number'
            value={form.total_tokens}
            onChange={(e) => set('total_tokens', Number(e.target.value))}
          />
        </div>

        <div className='flex flex-col gap-1.5'>
          <Label htmlFor='tp-w1-seconds'>
            {t('Window 1 duration (seconds)')}
          </Label>
          <Input
            id='tp-w1-seconds'
            type='number'
            value={form.window1_seconds}
            onChange={(e) => set('window1_seconds', Number(e.target.value))}
          />
          <p className='text-muted-foreground text-[11px]'>
            = {formatWindowDuration(form.window1_seconds)}
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
          <Label htmlFor='tp-w2-seconds'>
            {t('Window 2 duration (seconds)')}
          </Label>
          <Input
            id='tp-w2-seconds'
            type='number'
            value={form.window2_seconds}
            onChange={(e) => set('window2_seconds', Number(e.target.value))}
          />
          <p className='text-muted-foreground text-[11px]'>
            = {formatWindowDuration(form.window2_seconds)}
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
          <Label htmlFor='tp-sort'>{t('Sort order')}</Label>
          <Input
            id='tp-sort'
            type='number'
            value={form.sort_order}
            onChange={(e) => set('sort_order', Number(e.target.value))}
          />
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

        <div className='flex items-center gap-2 sm:col-span-2'>
          <Switch
            id='tp-enabled'
            checked={form.enabled}
            onCheckedChange={(checked) => set('enabled', checked)}
          />
          <Label htmlFor='tp-enabled'>{t('Enabled')}</Label>
        </div>
      </div>
    </Dialog>
  )
}

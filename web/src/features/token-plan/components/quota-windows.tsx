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
import { useTranslation } from 'react-i18next'

import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Progress } from '@/components/ui/progress'
import { cn } from '@/lib/utils'

import {
  formatCountdown,
  formatNumber,
  formatUnixTime,
  formatWindowDuration,
} from '../lib/format'
import type { TokenPlanWindow } from '../types'

function toneClass(percent: number): string {
  if (percent >= 95) return 'text-destructive'
  if (percent >= 80) return 'text-warning'
  return 'text-success'
}

function WindowCard({ data }: { data: TokenPlanWindow }) {
  const { t } = useTranslation()
  const percent = Math.max(0, Math.min(100, data.used_percent ?? 0))
  const tone = toneClass(percent)

  return (
    <Card size='sm' className='gap-0 py-0'>
      <CardHeader className='p-4 pb-2'>
        <div className='flex items-start justify-between gap-3'>
          <div className='min-w-0'>
            <CardTitle className='text-sm font-semibold'>
              {t('{{duration}} window', {
                duration: formatWindowDuration(data.limit_window_seconds),
              })}
            </CardTitle>
            <CardDescription className='mt-1 text-xs'>
              {t('Rolling from first use')}
            </CardDescription>
          </div>
          <div className='shrink-0 text-right'>
            <div
              className={cn(
                'text-2xl leading-none font-semibold tabular-nums',
                tone
              )}
            >
              {percent}%
            </div>
            <div className='text-muted-foreground mt-1 text-[11px]'>
              {t('Used')}
            </div>
          </div>
        </div>
      </CardHeader>
      <CardContent className='p-4 pt-0'>
        <Progress
          value={percent}
          aria-label={t('{{duration}} window usage: {{percent}}%', {
            duration: formatWindowDuration(data.limit_window_seconds),
            percent,
          })}
          className={cn(
            'mt-1',
            tone,
            '[&_[data-slot=progress-track]]:h-2',
            '[&_[data-slot=progress-indicator]]:bg-current'
          )}
        />

        <div className='mt-4 grid grid-cols-2 gap-3 text-xs'>
          <div>
            <div className='text-muted-foreground text-[11px]'>
              {t('Remaining')}
            </div>
            <div className='mt-0.5 font-medium tabular-nums'>
              {formatNumber(data.remaining_tokens)}
            </div>
          </div>
          <div>
            <div className='text-muted-foreground text-[11px]'>
              {t('Window limit')}
            </div>
            <div className='mt-0.5 font-medium tabular-nums'>
              {formatNumber(data.limit_tokens)}
            </div>
          </div>
          <div>
            <div className='text-muted-foreground text-[11px]'>
              {t('Resets at')}
            </div>
            <div className='mt-0.5 tabular-nums'>
              {formatUnixTime(data.reset_at)}
            </div>
          </div>
          <div>
            <div className='text-muted-foreground text-[11px]'>
              {t('Resets in')}
            </div>
            <div className='mt-0.5 tabular-nums'>
              {formatCountdown(data.reset_after_seconds)}
            </div>
          </div>
        </div>

        {data.exhausted ? (
          <p className='text-destructive mt-3 text-xs'>
            {t(
              'This window is used up. Requests are rejected until it resets.'
            )}
          </p>
        ) : null}
      </CardContent>
    </Card>
  )
}

export function QuotaWindows({ windows }: { windows: TokenPlanWindow[] }) {
  if (!windows.length) return null
  return (
    <div className='grid gap-3 md:grid-cols-2'>
      {windows.map((w) => (
        <WindowCard key={w.key} data={w} />
      ))}
    </div>
  )
}

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
import { Check, Crown, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { cn } from '@/lib/utils'

import { formatNumber, formatWindowDuration } from '../lib/format'
import type { TokenPlan } from '../types'

function currencySymbol(currency: string): string {
  switch ((currency || '').toUpperCase()) {
    case 'CNY':
    case 'RMB':
      return '¥'
    case 'USD':
      return '$'
    case 'EUR':
      return '€'
    default:
      return ''
  }
}

type PlanCardsProps = {
  plans: TokenPlan[]
  currentPlanId?: number
  purchasingId?: number
  onPurchase: (planId: number) => void
}

export function PlanCards({
  plans,
  currentPlanId,
  purchasingId,
  onPurchase,
}: PlanCardsProps) {
  const { t } = useTranslation()

  if (!plans.length) {
    return (
      <Card>
        <CardContent className='text-muted-foreground py-10 text-center text-sm'>
          {t('No plans available yet. Ask an administrator to publish one.')}
        </CardContent>
      </Card>
    )
  }

  // 列数跟随实际套餐数量，避免只有一两个套餐时右侧留出一大片空白。
  const columnClass = [
    'md:grid-cols-1',
    'md:grid-cols-1',
    'md:grid-cols-2',
    'md:grid-cols-3',
  ][Math.min(plans.length, 3)]

  return (
    <div
      className={cn(
        'grid gap-4',
        columnClass,
        plans.length === 1 && 'mx-auto w-full max-w-sm'
      )}
    >
      {plans.map((plan) => {
        const isCurrent = currentPlanId === plan.id
        const isPurchasing = purchasingId === plan.id
        return (
          <Card
            key={plan.id}
            className={cn('flex flex-col', isCurrent && 'ring-primary ring-2')}
          >
            <CardHeader>
              <div className='flex items-center justify-between gap-2'>
                <CardTitle className='flex items-center gap-2 text-base'>
                  <Crown className='text-primary h-4 w-4' />
                  {plan.title}
                </CardTitle>
                {isCurrent ? (
                  <Badge variant='secondary'>{t('Current')}</Badge>
                ) : null}
              </div>
              <CardDescription className='min-h-8 text-xs'>
                {plan.subtitle}
              </CardDescription>
              <div className='mt-2 flex items-baseline gap-1'>
                <span className='text-3xl font-semibold tabular-nums'>
                  {currencySymbol(plan.currency)}
                  {plan.price_amount}
                </span>
                <span className='text-muted-foreground text-xs'>
                  / {plan.duration_days} {t('days')}
                </span>
              </div>
            </CardHeader>
            <CardContent className='flex-1'>
              <ul className='space-y-2 text-xs'>
                <li className='flex items-start gap-2'>
                  <Check className='text-success mt-0.5 h-3.5 w-3.5 shrink-0' />
                  <span>
                    {t('Total quota')}:{' '}
                    <span className='font-medium tabular-nums'>
                      {formatNumber(plan.total_tokens)}
                    </span>{' '}
                    {t('tokens')}
                  </span>
                </li>
                <li className='flex items-start gap-2'>
                  <Check className='text-success mt-0.5 h-3.5 w-3.5 shrink-0' />
                  <span>
                    {formatWindowDuration(plan.window1_minutes * 60)}{' '}
                    {t('window')}:{' '}
                    <span className='font-medium tabular-nums'>
                      {formatNumber(plan.window1_tokens)}
                    </span>
                  </span>
                </li>
                <li className='flex items-start gap-2'>
                  <Check className='text-success mt-0.5 h-3.5 w-3.5 shrink-0' />
                  <span>
                    {formatWindowDuration(plan.window2_minutes * 60)}{' '}
                    {t('window')}:{' '}
                    <span className='font-medium tabular-nums'>
                      {formatNumber(plan.window2_tokens)}
                    </span>
                  </span>
                </li>
                <li className='flex items-start gap-2'>
                  <Check className='text-success mt-0.5 h-3.5 w-3.5 shrink-0' />
                  <span>
                    {plan.metering_mode === 'weighted'
                      ? t('Metered by model ratio')
                      : t('Metered by raw tokens')}
                  </span>
                </li>
              </ul>
            </CardContent>
            <CardFooter>
              <Button
                className='w-full'
                disabled={!!purchasingId}
                onClick={() => onPurchase(plan.id)}
              >
                {isPurchasing ? (
                  <Loader2 className='h-4 w-4 animate-spin' />
                ) : null}
                {isCurrent ? t('Renew') : t('Subscribe Now')}
              </Button>
            </CardFooter>
          </Card>
        )
      })}
    </div>
  )
}

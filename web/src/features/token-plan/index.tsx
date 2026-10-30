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
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import {
  getTokenPlanPlans,
  getTokenPlanSelf,
  purchaseTokenPlan,
  resetTokenPlanKey,
} from './api'
import { AvailableModels } from './components/available-models'
import { DedicatedKeyCard } from './components/dedicated-key-card'
import { PlanCards } from './components/plan-cards'
import { QuotaWindows } from './components/quota-windows'
import { formatUnixTime } from './lib/format'

const QUERY_KEY = ['token-plan']

export function TokenPlan() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  // 已订阅时套餐列表收进弹窗，避免把在售套餐和当前套餐混在一起。
  const [plansOpen, setPlansOpen] = useState(false)

  // 套餐与订阅状态都可能被管理员在后台改动（删除套餐会直接取消订阅），
  // 所以不吃全局的 staleTime 缓存：每次进入页面都重新拉，切回标签页也重新拉。
  const plansQuery = useQuery({
    queryKey: [...QUERY_KEY, 'plans'],
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: async () => {
      const result = await getTokenPlanPlans()
      if (!result.success) {
        throw createServerError(result, t('Failed to load plans'))
      }
      return result.data ?? []
    },
  })

  const selfQuery = useQuery({
    queryKey: [...QUERY_KEY, 'self'],
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: async () => {
      const result = await getTokenPlanSelf()
      if (!result.success) {
        throw createServerError(result, t('Failed to load your TOKEN PLAN'))
      }
      return result.data
    },
  })

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: QUERY_KEY })

  const purchaseMutation = useMutation({
    mutationFn: async (planId: number) => {
      const result = await purchaseTokenPlan(planId)
      if (!result.success) {
        throw createServerError(result, t('Subscription failed'))
      }
      return result.data
    },
    onSuccess: () => {
      toast.success(t('Subscribed successfully'))
      // 购买后关闭弹窗，页面回到「已订阅」视图。
      setPlansOpen(false)
      invalidate()
    },
    onError: (error) => handleServerError(error, t('Subscription failed')),
  })

  const resetMutation = useMutation({
    mutationFn: async () => {
      const result = await resetTokenPlanKey()
      if (!result.success) {
        throw createServerError(result, t('Failed to reset the key'))
      }
      return result.data
    },
    onSuccess: () => {
      toast.success(t('Key reset successfully'))
      invalidate()
    },
    onError: (error) => handleServerError(error, t('Failed to reset the key')),
  })

  const self = selfQuery.data
  const hasPlan = !!self?.has_plan && !!self.subscription

  const windowsSection = (() => {
    if (selfQuery.isLoading) {
      return (
        <div className='grid gap-3 md:grid-cols-2'>
          <Skeleton className='h-40 w-full' />
          <Skeleton className='h-40 w-full' />
        </div>
      )
    }
    if (hasPlan && self?.windows?.length) {
      return <QuotaWindows windows={self.windows} />
    }
    return null
  })()

  const planCards = plansQuery.isLoading ? (
    <div className='grid gap-4 md:grid-cols-3'>
      <Skeleton className='h-64 w-full' />
      <Skeleton className='h-64 w-full' />
      <Skeleton className='h-64 w-full' />
    </div>
  ) : (
    <PlanCards
      plans={plansQuery.data ?? []}
      currentPlanId={self?.subscription?.plan_id}
      purchasingId={
        purchaseMutation.isPending ? purchaseMutation.variables : undefined
      }
      onPurchase={(planId) => purchaseMutation.mutate(planId)}
    />
  )

  // 弹窗宽度跟着套餐数量走，一两个套餐时不会留下大片空白。
  const plansDialogWidth = [
    'sm:max-w-md',
    'sm:max-w-md',
    'sm:max-w-3xl',
    'sm:max-w-5xl',
  ][Math.min(plansQuery.data?.length ?? 0, 3)]

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>TOKEN PLAN</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <Button
          variant='outline'
          size='sm'
          disabled={plansQuery.isFetching || selfQuery.isFetching}
          onClick={invalidate}
        >
          <RefreshCw
            className={`h-4 w-4 ${
              plansQuery.isFetching || selfQuery.isFetching
                ? 'animate-spin'
                : ''
            }`}
          />
          {t('Refresh')}
        </Button>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='mx-auto flex w-full max-w-5xl flex-col gap-4 pb-6'>
          {hasPlan && self?.subscription ? (
            <div className='bg-muted/40 flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3 text-xs'>
              <div className='flex items-center gap-2'>
                <span className='font-semibold'>
                  {self.subscription.plan_title}
                </span>
                <span className='text-muted-foreground'>
                  {t('Expires at')} {formatUnixTime(self.subscription.end_time)}
                </span>
              </div>
              <Button
                variant='outline'
                size='sm'
                onClick={() => setPlansOpen(true)}
              >
                <RefreshCw className='h-3.5 w-3.5' />
                {t('Change plan')}
              </Button>
            </div>
          ) : null}

          {windowsSection}

          {hasPlan ? null : (
            <div>
              <h3 className='mb-2 text-sm font-semibold'>{t('Plans')}</h3>
              {planCards}
            </div>
          )}

          {hasPlan ? (
            <>
              <DedicatedKeyCard
                apiKey={self?.key?.key ?? ''}
                resetting={resetMutation.isPending}
                onReset={() => resetMutation.mutate()}
              />
              <AvailableModels
                models={self?.models ?? []}
                unrestricted={self?.unrestricted}
              />
            </>
          ) : null}
        </div>

        <Dialog
          open={plansOpen}
          onOpenChange={setPlansOpen}
          title={t('Change plan')}
          description={t(
            'Switching plans cancels the current one immediately. Unused quota is not carried over.'
          )}
          contentClassName={plansDialogWidth}
        >
          {planCards}
        </Dialog>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

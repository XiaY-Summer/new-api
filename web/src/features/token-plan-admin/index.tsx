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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Pencil, Plus, RotateCcw, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  formatNumber,
  formatUnixTime,
  formatWindowDuration,
} from '@/features/token-plan/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import {
  deleteTokenPlan,
  getAdminTokenPlans,
  getTokenPlanSubscribers,
  resetTokenPlanUsage,
} from './api'
import { PlanFormDialog } from './components/plan-form-dialog'
import type { TokenPlanAdminRecord, TokenPlanSubscriber } from './types'

const QUERY_KEY = ['token-plan-admin', 'plans']
const SUBSCRIBERS_QUERY_KEY = ['token-plan-admin', 'subscribers']

/** 重置目标：单个订阅者，或 'all' 表示全体。 */
type ResetTarget = TokenPlanSubscriber | 'all' | null

export function TokenPlans() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [formOpen, setFormOpen] = useState(false)
  const [editing, setEditing] = useState<TokenPlanAdminRecord | null>(null)
  const [deleting, setDeleting] = useState<TokenPlanAdminRecord | null>(null)
  const [removing, setRemoving] = useState(false)
  const [resetTarget, setResetTarget] = useState<ResetTarget>(null)
  const [resetting, setResetting] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: QUERY_KEY,
    queryFn: async () => {
      const result = await getAdminTokenPlans()
      if (!result.success) {
        throw createServerError(result, t('Failed to load plans'))
      }
      return result.data ?? []
    },
  })

  const subscribersQuery = useQuery({
    queryKey: SUBSCRIBERS_QUERY_KEY,
    queryFn: async () => {
      const result = await getTokenPlanSubscribers()
      if (!result.success) {
        throw createServerError(result, t('Failed to load subscribers'))
      }
      return result.data ?? []
    },
  })

  const refresh = () => queryClient.invalidateQueries({ queryKey: QUERY_KEY })

  const handleDelete = async () => {
    if (!deleting) return
    setRemoving(true)
    try {
      const result = await deleteTokenPlan(deleting.id)
      if (!result.success) {
        throw createServerError(result, t('Failed to delete the plan'))
      }
      toast.success(t('Plan deleted'))
      setDeleting(null)
      refresh()
    } catch (error) {
      handleServerError(error, t('Failed to delete the plan'))
    } finally {
      setRemoving(false)
    }
  }

  const handleReset = async () => {
    if (!resetTarget) return
    setResetting(true)
    try {
      const userId = resetTarget === 'all' ? undefined : resetTarget.user_id
      const result = await resetTokenPlanUsage(userId)
      if (!result.success) {
        throw createServerError(result, t('Failed to reset usage'))
      }
      toast.success(
        t('Reset usage for {{count}} subscription(s)', {
          count: result.data?.affected ?? 0,
        })
      )
      setResetTarget(null)
      queryClient.invalidateQueries({ queryKey: SUBSCRIBERS_QUERY_KEY })
    } catch (error) {
      handleServerError(error, t('Failed to reset usage'))
    } finally {
      setResetting(false)
    }
  }

  const plans = data ?? []
  const subscribers = subscribersQuery.data ?? []

  return (
    <>
      <SectionPageLayout>
        <SectionPageLayout.Title>
          {t('Subscription Plan Management')}
        </SectionPageLayout.Title>
        <SectionPageLayout.Actions>
          <Button
            size='sm'
            onClick={() => {
              setEditing(null)
              setFormOpen(true)
            }}
          >
            <Plus className='h-4 w-4' />
            {t('Create plan')}
          </Button>
        </SectionPageLayout.Actions>
        <SectionPageLayout.Content>
          <div className='mx-auto w-full max-w-6xl pb-6'>
            <p className='text-muted-foreground mb-3 text-xs'>
              {t(
                'These plans drive the TOKEN PLAN page. Both windows are anchored to the first request and reset every N minutes.'
              )}
            </p>

            {isLoading ? (
              <Skeleton className='h-64 w-full' />
            ) : (
              <div className='overflow-x-auto rounded-lg border'>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t('Plan')}</TableHead>
                      <TableHead className='text-right'>{t('Price')}</TableHead>
                      <TableHead className='text-right'>
                        {t('Total quota')}
                      </TableHead>
                      <TableHead className='text-right'>
                        {`${formatWindowDuration((plans[0]?.window1_minutes ?? 0) * 60)} ${t('window')}`}
                      </TableHead>
                      <TableHead className='text-right'>
                        {`${formatWindowDuration((plans[0]?.window2_minutes ?? 0) * 60)} ${t('window')}`}
                      </TableHead>
                      <TableHead>{t('Groups')}</TableHead>
                      <TableHead>{t('Metering mode')}</TableHead>
                      <TableHead>{t('Status')}</TableHead>
                      <TableHead className='text-right'>
                        {t('Actions')}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {plans.length === 0 ? (
                      <TableRow>
                        <TableCell
                          colSpan={9}
                          className='text-muted-foreground py-10 text-center text-sm'
                        >
                          {t('No plans yet. Create one to get started.')}
                        </TableCell>
                      </TableRow>
                    ) : (
                      plans.map((plan) => (
                        <TableRow key={plan.id}>
                          <TableCell>
                            <div className='font-medium'>{plan.title}</div>
                            <div className='text-muted-foreground max-w-64 truncate text-xs'>
                              {plan.subtitle}
                            </div>
                          </TableCell>
                          <TableCell className='text-right tabular-nums'>
                            ¥{plan.price_amount}
                          </TableCell>
                          <TableCell className='text-right tabular-nums'>
                            {formatNumber(plan.total_tokens)}
                          </TableCell>
                          <TableCell className='text-right tabular-nums'>
                            {formatNumber(plan.window1_tokens)}
                          </TableCell>
                          <TableCell className='text-right tabular-nums'>
                            {formatNumber(plan.window2_tokens)}
                          </TableCell>
                          <TableCell className='max-w-40 truncate text-xs'>
                            {plan.group_limits || t('All groups')}
                          </TableCell>
                          <TableCell className='text-xs'>
                            {plan.metering_mode === 'weighted'
                              ? t('Weighted')
                              : t('Raw')}
                          </TableCell>
                          <TableCell>
                            <Badge
                              variant={plan.enabled ? 'secondary' : 'outline'}
                            >
                              {plan.enabled ? t('Enabled') : t('Disabled')}
                            </Badge>
                          </TableCell>
                          <TableCell className='text-right'>
                            <div className='flex justify-end gap-1'>
                              <Button
                                variant='ghost'
                                size='icon'
                                aria-label={t('Edit plan')}
                                onClick={() => {
                                  setEditing(plan)
                                  setFormOpen(true)
                                }}
                              >
                                <Pencil className='h-4 w-4' />
                              </Button>
                              <Button
                                variant='ghost'
                                size='icon'
                                aria-label={t('Delete plan')}
                                onClick={() => setDeleting(plan)}
                              >
                                <Trash2 className='text-destructive h-4 w-4' />
                              </Button>
                            </div>
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>
            )}

            <div className='mt-8'>
              <div className='mb-3 flex flex-wrap items-start justify-between gap-2'>
                <div>
                  <h3 className='text-sm font-semibold'>{t('Subscribers')}</h3>
                  <p className='text-muted-foreground mt-1 text-xs'>
                    {t(
                      'Resetting usage clears consumed tokens and restarts both windows from the next request.'
                    )}
                  </p>
                </div>
                <Button
                  variant='outline'
                  size='sm'
                  disabled={!subscribers.length}
                  onClick={() => setResetTarget('all')}
                >
                  <RotateCcw className='h-4 w-4' />
                  {t('Reset all usage')}
                </Button>
              </div>

              {subscribersQuery.isLoading ? (
                <Skeleton className='h-40 w-full' />
              ) : (
                <div className='overflow-x-auto rounded-lg border'>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('User')}</TableHead>
                        <TableHead>{t('Plan')}</TableHead>
                        <TableHead className='text-right'>
                          {t('Total quota')}
                        </TableHead>
                        <TableHead className='text-right'>
                          {t('Window 1')}
                        </TableHead>
                        <TableHead className='text-right'>
                          {t('Window 2')}
                        </TableHead>
                        <TableHead>{t('Expires at')}</TableHead>
                        <TableHead className='text-right'>
                          {t('Actions')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {subscribers.length === 0 ? (
                        <TableRow>
                          <TableCell
                            colSpan={7}
                            className='text-muted-foreground py-10 text-center text-sm'
                          >
                            {t('No active subscriptions yet.')}
                          </TableCell>
                        </TableRow>
                      ) : (
                        subscribers.map((sub) => (
                          <TableRow key={sub.user_id}>
                            <TableCell>
                              <div className='font-medium'>
                                {sub.username || `#${sub.user_id}`}
                              </div>
                              <div className='text-muted-foreground text-xs'>
                                ID {sub.user_id}
                              </div>
                            </TableCell>
                            <TableCell className='text-xs'>
                              {sub.plan_title}
                            </TableCell>
                            <TableCell className='text-right text-xs tabular-nums'>
                              {formatNumber(sub.used_tokens)} /{' '}
                              {formatNumber(sub.total_tokens)}
                            </TableCell>
                            <TableCell className='text-right text-xs tabular-nums'>
                              {formatNumber(sub.window1_used)} /{' '}
                              {formatNumber(sub.window1_limit)}
                            </TableCell>
                            <TableCell className='text-right text-xs tabular-nums'>
                              {formatNumber(sub.window2_used)} /{' '}
                              {formatNumber(sub.window2_limit)}
                            </TableCell>
                            <TableCell className='text-xs tabular-nums'>
                              {formatUnixTime(sub.end_time)}
                            </TableCell>
                            <TableCell className='text-right'>
                              <Button
                                variant='ghost'
                                size='sm'
                                onClick={() => setResetTarget(sub)}
                              >
                                <RotateCcw className='h-3.5 w-3.5' />
                                {t('Reset')}
                              </Button>
                            </TableCell>
                          </TableRow>
                        ))
                      )}
                    </TableBody>
                  </Table>
                </div>
              )}
            </div>
          </div>
        </SectionPageLayout.Content>
      </SectionPageLayout>

      <PlanFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        initial={editing}
        onSaved={refresh}
      />

      <ConfirmDialog
        open={!!deleting}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t('Delete plan')}
        desc={t(
          'Deleting a plan does not affect users who already subscribed to it, but it can no longer be purchased.'
        )}
        confirmText={t('Delete')}
        destructive
        isLoading={removing}
        handleConfirm={handleDelete}
      />

      <ConfirmDialog
        open={!!resetTarget}
        onOpenChange={(open) => !open && setResetTarget(null)}
        title={t('Reset usage')}
        desc={
          resetTarget === 'all'
            ? t(
                'This clears consumed tokens for every active subscription and restarts their windows. It cannot be undone.'
              )
            : t(
                'This clears the consumed tokens of this subscription and restarts its windows. It cannot be undone.'
              )
        }
        confirmText={t('Reset')}
        destructive
        isLoading={resetting}
        handleConfirm={handleReset}
      />
    </>
  )
}

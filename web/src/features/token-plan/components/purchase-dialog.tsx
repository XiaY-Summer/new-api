import { useMutation, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Combobox } from '@/components/ui/combobox'
import { getTopupInfo } from '@/features/wallet/api'
import { submitPaymentForm } from '@/features/wallet/lib/payment'
import { handleServerError } from '@/lib/handle-server-error'
import {
  createServerError,
  requireServerSuccess,
} from '@/lib/server-error-message'

import { purchaseTokenPlan } from '../api'
import type { TokenPlan } from '../types'

type Props = {
  plan: TokenPlan | null
  onOpenChange: (open: boolean) => void
}

const OTHER_PROVIDERS = new Set([
  'stripe',
  'creem',
  'waffo',
  'waffo_pancake',
  'balance',
])

export function TokenPlanPurchaseDialog(props: Props) {
  const { t } = useTranslation()
  const [method, setMethod] = useState('')
  const paymentQuery = useQuery({
    queryKey: ['token-plan', 'payment-info'],
    enabled: !!props.plan,
    staleTime: 0,
    queryFn: async () => requireServerSuccess(await getTopupInfo()).data,
  })
  const methods = paymentQuery.data?.enable_online_topup
    ? (paymentQuery.data.pay_methods ?? []).filter(
        (item) => !OTHER_PROVIDERS.has(item.type)
      )
    : []
  const selectedMethod =
    methods.find((item) => item.type === method) ?? methods[0]

  const payment = useMutation({
    mutationFn: async () => {
      if (!props.plan || !selectedMethod) {
        throw new Error(t('Please select a payment method'))
      }
      const result = await purchaseTokenPlan(props.plan.id, selectedMethod.type)
      if (!result.success || !result.url || !result.data) {
        throw createServerError(result, t('Payment request failed'))
      }
      submitPaymentForm(result.url, result.data)
    },
    onSuccess: () => {
      toast.success(t('Payment initiated'))
      props.onOpenChange(false)
    },
    onError: (error) => handleServerError(error, t('Payment request failed')),
  })

  return (
    <ConfirmDialog
      open={!!props.plan}
      onOpenChange={(open) => {
        if (!payment.isPending) props.onOpenChange(open)
      }}
      title={t('Confirm Payment')}
      desc={
        props.plan
          ? `${props.plan.title} · ${props.plan.currency} ${props.plan.price_amount.toFixed(2)}`
          : ''
      }
      confirmText={t('Confirm Payment')}
      handleConfirm={() => payment.mutate()}
      isLoading={payment.isPending}
      disabled={
        paymentQuery.isFetching ||
        paymentQuery.isError ||
        !selectedMethod ||
        !['CNY', 'RMB'].includes(props.plan?.currency ?? '')
      }
    >
      <div className='space-y-3'>
        {paymentQuery.isError ? (
          <p role='alert'>{t('Payment request failed')}</p>
        ) : null}
        {!paymentQuery.isFetching &&
        !paymentQuery.isError &&
        !methods.length ? (
          <p role='status'>
            {t('No payment methods available. Please contact administrator.')}
          </p>
        ) : null}
        {props.plan && !['CNY', 'RMB'].includes(props.plan.currency) ? (
          <p role='alert'>{t('EPay only supports CNY plans')}</p>
        ) : null}
        <Combobox
          aria-label={t('Payment Method')}
          options={methods.map((item) => ({
            value: item.type,
            label: item.name || item.type,
          }))}
          value={selectedMethod?.type ?? ''}
          onValueChange={(value) => setMethod(value ?? '')}
          placeholder={t('Please select a payment method')}
          disabled={
            paymentQuery.isFetching || payment.isPending || !methods.length
          }
        />
        <p className='text-muted-foreground text-sm'>
          {t('TOKEN PLAN activates after payment is confirmed')}
        </p>
      </div>
    </ConfirmDialog>
  )
}

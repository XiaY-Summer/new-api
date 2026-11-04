import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import type { ColumnDef } from '@tanstack/react-table'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'
import { toIntlLocale } from '@/i18n/languages'
import { api } from '@/lib/api'
import { formatLocalCurrencyAmount } from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { handleServerError } from '@/lib/handle-server-error'
import { requireServerSuccess } from '@/lib/server-error-message'

import { formatUnixTime } from '../lib/format'
import type { ApiResponse } from '../types'

type PaymentIssue = {
  id: number
  user_id: number
  trade_no: string
  money: number
  paid_time: number
  fulfillment_error: string
}

type PaymentIssuesPage = { items: PaymentIssue[]; total: number }

export function TokenPlanPaymentIssues(props: { admin?: boolean }) {
  const { t, i18n } = useTranslation()
  const locale = toIntlLocale(i18n.resolvedLanguage || i18n.language)
  const client = useQueryClient()
  const [pagination, setPagination] = useState({ pageIndex: 0, pageSize: 10 })
  const endpoint = props.admin
    ? '/api/token-plan/admin/payment-issues'
    : '/api/token-plan/payment-issues'
  const issues = useQuery({
    queryKey: ['token-plan', 'payment-issues', !!props.admin, pagination],
    staleTime: 0,
    refetchOnWindowFocus: true,
    queryFn: async () => {
      const response = await api.get<ApiResponse<PaymentIssuesPage>>(endpoint, {
        params: { p: pagination.pageIndex + 1, page_size: pagination.pageSize },
      })
      return requireServerSuccess(response.data).data
    },
  })
  const retry = useMutation({
    mutationFn: async (tradeNo: string) =>
      requireServerSuccess(
        (
          await api.post(
            `/api/token-plan/admin/orders/${encodeURIComponent(tradeNo)}/retry`
          )
        ).data
      ),
    onSuccess: () => toast.success(t('Subscription purchased successfully')),
    onError: (error) => handleServerError(error),
    onSettled: async () => {
      await client.invalidateQueries({ queryKey: ['token-plan'] })
      await client.invalidateQueries({
        queryKey: ['token-plan-admin', 'subscribers'],
      })
    },
  })
  const columns: ColumnDef<PaymentIssue>[] = [
    {
      accessorKey: 'trade_no',
      header: t('Order number'),
      meta: { mobileTitle: true },
    },
    {
      accessorKey: 'money',
      header: t('Amount'),
      cell: ({ row }) =>
        `CNY ${formatLocalCurrencyAmount(row.original.money, { showSymbol: false, abbreviate: false, locale })}`,
    },
    {
      accessorKey: 'paid_time',
      header: t('Payment time'),
      cell: ({ row }) => formatUnixTime(row.original.paid_time),
    },
  ]
  if (props.admin) {
    columns.push(
      {
        accessorKey: 'user_id',
        header: t('User ID'),
        cell: ({ row }) => formatNumber(row.original.user_id, locale),
      },
      {
        accessorKey: 'fulfillment_error',
        header: t('Activation failure reason'),
      },
      {
        id: 'retry',
        header: t('Actions'),
        cell: ({ row }) => (
          <Button
            size='sm'
            variant='outline'
            disabled={retry.isPending}
            onClick={() => retry.mutate(row.original.trade_no)}
          >
            {t('Retry activation')}
          </Button>
        ),
      }
    )
  }
  const { table } = useDataTable({
    data: issues.data?.items ?? [],
    columns,
    totalCount: issues.data?.total ?? 0,
    pagination,
    onPaginationChange: setPagination,
    manualPagination: true,
    enableSorting: false,
    enableRowSelection: false,
    getRowId: (row) => String(row.id),
    ensurePageInRange: (pageCount) => {
      if (
        issues.isSuccess &&
        !issues.isFetching &&
        pagination.pageIndex >= Math.max(1, pageCount)
      ) {
        setPagination((previous) => ({
          ...previous,
          pageIndex: Math.max(0, pageCount - 1),
        }))
      }
    },
  })

  if (issues.isError) {
    return (
      <ErrorState
        title={t('Failed to load payment issues')}
        onRetry={() => issues.refetch()}
      />
    )
  }
  if (!props.admin && !issues.isLoading && !issues.data?.total) return null

  return (
    <section
      className='mb-4 space-y-3'
      aria-label={t('Paid orders awaiting activation')}
    >
      <div className='flex items-center justify-between gap-2'>
        <h3 className='text-sm font-semibold'>
          {t('Paid orders awaiting activation')}
        </h3>
        <Button
          variant='outline'
          size='sm'
          disabled={issues.isFetching}
          onClick={() => issues.refetch()}
        >
          {t('Refresh')}
        </Button>
      </div>
      {issues.data?.total ? (
        <p className='text-muted-foreground text-sm'>
          {t(
            'Payment received. Your TOKEN PLAN is awaiting activation. Please contact the administrator with your order number.'
          )}
        </p>
      ) : null}
      <DataTablePage
        table={table}
        columns={columns}
        isLoading={issues.isLoading}
        isFetching={issues.isFetching}
        toolbarProps={null}
        emptyTitle={t('No orders awaiting activation')}
        fixedHeight={false}
        paginationInFooter={false}
        compactPagination
      />
    </section>
  )
}

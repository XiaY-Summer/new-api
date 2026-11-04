import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import i18next from 'i18next'
import { beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'
import { Route as TokenPlanRoute } from '@/routes/_authenticated/token-plan'

import { TokenPlan } from '..'
import { TokenPlanPaymentIssues } from '../components/payment-issues'

beforeEach(() => {
  vi.spyOn(api, 'get').mockImplementation(async (url) => ({
    data: {
      success: true,
      data: url.endsWith('/self') ? { has_plan: false } : [],
    },
  }))
})

test.each([
  ['success', 'Subscription purchased successfully'],
  [
    'processing',
    'Payment received. Your TOKEN PLAN is awaiting activation. Please contact the administrator with your order number.',
  ],
  [
    'pending',
    'Payment is pending. Refresh this page after completing payment.',
  ],
  [
    'fail',
    'Payment could not be confirmed. If charged, contact the administrator with your order number.',
  ],
])(
  'returning with pay=%s shows the payment result',
  async (status, message) => {
    const root = createRootRoute()
    const authenticated = createRoute({
      getParentRoute: () => root,
      id: '_authenticated',
    })
    const plan = createRoute({
      getParentRoute: () => authenticated,
      path: 'token-plan/',
      component: TokenPlan,
      validateSearch: TokenPlanRoute.options.validateSearch,
    })
    const router = createRouter({
      routeTree: root.addChildren([authenticated.addChildren([plan])]),
      history: createMemoryHistory({
        initialEntries: [`/token-plan?pay=${status}`],
      }),
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
    expect(await screen.findByText(message)).toBeVisible()
    if (status !== 'success') {
      expect(
        screen.queryByText('Subscription purchased successfully')
      ).not.toBeInTheDocument()
    }
  }
)

const issue = {
  id: 1,
  user_id: 7,
  trade_no: 'TP-paid-awaiting-activation',
  money: 59.25,
  paid_time: 1793700000,
  fulfillment_error: 'template unavailable',
}

function renderPaymentIssues(admin = false) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <TokenPlanPaymentIssues admin={admin} />
    </QueryClientProvider>
  )
}

test('users can find a paid order number without seeing admin diagnostics or retry controls', async () => {
  vi.mocked(api.get).mockResolvedValue({
    data: { success: true, data: { items: [issue], total: 1 } },
  })
  renderPaymentIssues()
  expect(await screen.findByText(issue.trade_no)).toBeVisible()
  expect(screen.getByText('CNY 59.25')).toBeVisible()
  expect(screen.queryByText(issue.fulfillment_error)).not.toBeInTheDocument()
  expect(
    screen.queryByRole('button', { name: 'Retry activation' })
  ).not.toBeInTheDocument()
  expect(api.get).toHaveBeenCalledWith('/api/token-plan/payment-issues', {
    params: { p: 1, page_size: 10 },
  })
})

test.each([true, false])(
  'administrator retry refreshes persisted order state (success=%s)',
  async (success) => {
    let settled = false
    vi.mocked(api.get).mockImplementation(async () => ({
      data: {
        success: true,
        data: { items: settled ? [] : [issue], total: settled ? 0 : 1 },
      },
    }))
    const post = vi.spyOn(api, 'post').mockImplementation(async () => {
      settled = success
      return {
        data: { success, message: success ? '' : 'Activation still pending' },
      }
    })
    const user = userEvent.setup()
    renderPaymentIssues(true)
    expect(await screen.findByText(issue.fulfillment_error)).toBeVisible()
    await user.click(screen.getByRole('button', { name: 'Retry activation' }))
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        `/api/token-plan/admin/orders/${issue.trade_no}/retry`
      )
    )
    if (success) {
      expect(
        await screen.findByText('No orders awaiting activation')
      ).toBeVisible()
      expect(screen.queryByText(issue.trade_no)).not.toBeInTheDocument()
    } else {
      await waitFor(() =>
        expect(
          screen.getByRole('button', { name: 'Retry activation' })
        ).toBeEnabled()
      )
      expect(screen.getByText(issue.trade_no)).toBeVisible()
    }
  }
)

test('payment issue loading errors can be retried and an empty user list is hidden', async () => {
  vi.mocked(api.get).mockRejectedValueOnce(new Error('Network unavailable'))
  const user = userEvent.setup()
  renderPaymentIssues()
  expect(await screen.findByText('Failed to load payment issues')).toBeVisible()
  vi.mocked(api.get).mockResolvedValue({
    data: { success: true, data: { items: [], total: 0 } },
  })
  await user.click(screen.getByRole('button', { name: 'Retry' }))
  await waitFor(() => {
    expect(
      screen.queryByText('Failed to load payment issues')
    ).not.toBeInTheDocument()
    expect(
      screen.queryByRole('region', { name: 'Paid orders awaiting activation' })
    ).not.toBeInTheDocument()
  })
})

test('retrying the last order on a page returns to remaining paid orders', async () => {
  let settled = false
  const firstPage = Array.from({ length: 10 }, (_, index) => ({
    ...issue,
    id: index + 2,
    trade_no: `remaining-paid-order-${index + 1}`,
  }))
  vi.mocked(api.get).mockImplementation(async (_url, config) => {
    const page = (config?.params as { p: number } | undefined)?.p
    let items = firstPage
    if (page === 2) items = settled ? [] : [issue]
    return {
      data: {
        success: true,
        data: { items, total: settled ? 10 : 11 },
      },
    }
  })
  vi.spyOn(api, 'post').mockImplementation(async () => {
    settled = true
    return { data: { success: true } }
  })
  const user = userEvent.setup()
  renderPaymentIssues(true)
  expect(await screen.findByText('remaining-paid-order-1')).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Go to next page' }))
  expect(await screen.findByText(issue.trade_no)).toBeVisible()
  await user.click(screen.getByRole('button', { name: 'Retry activation' }))
  expect(await screen.findByText('remaining-paid-order-1')).toBeVisible()
  expect(
    screen.queryByText('No orders awaiting activation')
  ).not.toBeInTheDocument()
})

test.each(['zhCN', 'zhTW'])(
  'payment amounts render with interface language %s',
  async (language) => {
    i18next.addResourceBundle(language, 'translation', {
      'Order number': 'Order number',
    })
    await i18next.changeLanguage(language)
    try {
      vi.mocked(api.get).mockResolvedValue({
        data: {
          success: true,
          data: { items: [{ ...issue, money: 1234.56 }], total: 1 },
        },
      })
      renderPaymentIssues()
      expect(await screen.findByText('CNY 1,234.56')).toBeVisible()
    } finally {
      await i18next.changeLanguage('en')
      i18next.removeResourceBundle(language, 'translation')
    }
  }
)

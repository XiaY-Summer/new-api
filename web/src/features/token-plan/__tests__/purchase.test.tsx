import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { beforeEach, expect, test, vi } from 'vitest'

import { api } from '@/lib/api'

import { TokenPlanPurchaseDialog } from '../components/purchase-dialog'
import type { TokenPlan } from '../types'

const plan: TokenPlan = {
  id: 7,
  title: 'Starter',
  subtitle: '',
  price_amount: 59,
  currency: 'CNY',
  duration_days: 30,
  total_tokens: 100,
  window1_minutes: 300,
  window1_tokens: 20,
  window2_minutes: 10080,
  window2_tokens: 60,
  metering_mode: 'raw',
  model_limits: '',
  group_limits: '',
  first_byte_delay_enabled: false,
  first_byte_delay_prompt_threshold: 0,
  first_byte_delay_probability: 0,
  first_byte_delay_min_ms: 0,
  first_byte_delay_max_ms: 0,
  concurrency_limit: 0,
  rate_limit_count: 0,
  rate_limit_window_seconds: 0,
  enabled: true,
}

function renderPurchase(selectedPlan: TokenPlan = plan) {
  const onOpenChange = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <TokenPlanPurchaseDialog
        plan={selectedPlan}
        onOpenChange={onOpenChange}
      />
    </QueryClientProvider>
  )
  return onOpenChange
}

beforeEach(() => {
  vi.spyOn(api, 'get').mockResolvedValue({
    data: {
      success: true,
      data: {
        enable_online_topup: true,
        pay_methods: [
          { type: 'alipay', name: 'Alipay' },
          { type: 'wxpay', name: 'WeChat' },
          { type: 'stripe', name: 'Stripe' },
        ],
      },
    },
  })
})

test('confirming the selected method submits EPay checkout without claiming activation', async () => {
  const user = userEvent.setup()
  const submit = vi
    .spyOn(HTMLFormElement.prototype, 'submit')
    .mockImplementation(() => {})
  const post = vi.spyOn(api, 'post').mockResolvedValue({
    data: {
      success: true,
      url: 'https://epay.example/submit.php',
      data: { money: '59.00', out_trade_no: 'TP-7' },
    },
  })
  const onOpenChange = renderPurchase()
  await user.click(
    await screen.findByRole('combobox', { name: 'Payment Method' })
  )
  await user.click(await screen.findByRole('option', { name: 'WeChat' }))
  await user.click(screen.getByRole('button', { name: 'Confirm Payment' }))
  await waitFor(() => expect(submit).toHaveBeenCalledOnce())
  expect(post).toHaveBeenCalledWith('/api/token-plan/purchase', {
    plan_id: 7,
    payment_method: 'wxpay',
  })
  expect(onOpenChange).toHaveBeenCalledWith(false)
  expect(
    screen.getByText('TOKEN PLAN activates after payment is confirmed')
  ).toBeVisible()
  expect(screen.queryByText('Subscribed successfully')).not.toBeInTheDocument()
})

test('missing EPay configuration disables checkout and explains the unavailable payment methods', async () => {
  vi.mocked(api.get).mockResolvedValue({
    data: {
      success: true,
      data: {
        enable_online_topup: false,
        pay_methods: [{ type: 'stripe', name: 'Stripe' }],
      },
    },
  })
  renderPurchase()
  expect(await screen.findByRole('status')).toHaveTextContent(
    'No payment methods available'
  )
  expect(screen.getByRole('button', { name: 'Confirm Payment' })).toBeDisabled()
  expect(
    screen.getByRole('combobox', { name: 'Payment Method' })
  ).toBeDisabled()
})

test('failed payment creation keeps checkout open and does not submit a payment form', async () => {
  const user = userEvent.setup()
  const submit = vi
    .spyOn(HTMLFormElement.prototype, 'submit')
    .mockImplementation(() => {})
  vi.spyOn(api, 'post').mockResolvedValue({
    data: { success: false, message: 'Payment request failed' },
  })
  const onOpenChange = renderPurchase()
  const confirm = screen.getByRole('button', { name: 'Confirm Payment' })
  await waitFor(() => expect(confirm).toBeEnabled())
  await user.click(confirm)
  await waitFor(() => expect(confirm).toBeEnabled())
  expect(submit).not.toHaveBeenCalled()
  expect(onOpenChange).not.toHaveBeenCalled()
})

test('a non-CNY plan cannot be purchased through EPay', async () => {
  renderPurchase({ ...plan, currency: 'USD' })
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'EPay only supports CNY plans'
  )
  expect(screen.getByRole('button', { name: 'Confirm Payment' })).toBeDisabled()
})

test('payment configuration failures leave checkout disabled', async () => {
  vi.mocked(api.get).mockRejectedValue(new Error('Gateway unavailable'))
  renderPurchase()
  expect(await screen.findByRole('alert')).toHaveTextContent(
    'Payment request failed'
  )
  expect(screen.getByRole('button', { name: 'Confirm Payment' })).toBeDisabled()
})

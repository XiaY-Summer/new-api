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

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export type TokenPlanMeteringMode = 'weighted' | 'raw'

export interface TokenPlanAdminRecord {
  id: number
  title: string
  subtitle: string
  price_amount: number
  currency: string
  duration_days: number
  total_tokens: number
  window1_minutes: number
  window1_tokens: number
  window2_minutes: number
  window2_tokens: number
  metering_mode: TokenPlanMeteringMode
  model_limits: string
  group_limits: string
  // 节流：延迟用毫秒、概率用 0-100、频率窗口用秒。
  first_byte_delay_enabled: boolean
  first_byte_delay_prompt_threshold: number
  first_byte_delay_probability: number
  first_byte_delay_min_ms: number
  first_byte_delay_max_ms: number
  concurrency_limit: number
  rate_limit_count: number
  rate_limit_window_seconds: number
  enabled: boolean
}

export interface TokenPlanPayload {
  plan: TokenPlanAdminRecord
}

export interface TokenPlanSubscriber {
  user_id: number
  username: string
  plan_title: string
  status: string
  total_tokens: number
  used_tokens: number
  window1_used: number
  window1_limit: number
  window2_used: number
  window2_limit: number
  start_time: number
  end_time: number
}

export function emptyTokenPlan(): TokenPlanAdminRecord {
  return {
    id: 0,
    title: '',
    subtitle: '',
    price_amount: 0,
    currency: 'CNY',
    duration_days: 30,
    total_tokens: 5_000_000,
    window1_minutes: 5 * 60,
    window1_tokens: 1_000_000,
    window2_minutes: 7 * 24 * 60,
    window2_tokens: 3_000_000,
    metering_mode: 'weighted',
    model_limits: '',
    group_limits: '',
    first_byte_delay_enabled: false,
    first_byte_delay_prompt_threshold: 0,
    first_byte_delay_probability: 100,
    first_byte_delay_min_ms: 300,
    first_byte_delay_max_ms: 800,
    concurrency_limit: 0,
    rate_limit_count: 0,
    rate_limit_window_seconds: 60,
    enabled: true,
  }
}

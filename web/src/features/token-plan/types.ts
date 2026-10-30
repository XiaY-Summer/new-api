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

export interface TokenPlan {
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
  enabled: boolean
}

export interface TokenPlanWindow {
  key: string
  limit_window_seconds: number
  limit_tokens: number
  used_tokens: number
  remaining_tokens: number
  used_percent: number
  reset_at: number
  reset_after_seconds: number
  exhausted: boolean
}

export interface TokenPlanSubscription {
  id: number
  plan_id: number
  plan_title: string
  status: string
  start_time: number
  end_time: number
  total_tokens: number
  used_tokens: number
  remaining_tokens: number
  used_percent: number
  anchor_at: number
  metering_mode: TokenPlanMeteringMode
}

export interface TokenPlanKeyInfo {
  id: number
  key: string
  name: string
  created_time: number
}

export interface TokenPlanSelf {
  has_plan: boolean
  subscription?: TokenPlanSubscription
  windows?: TokenPlanWindow[]
  key?: TokenPlanKeyInfo
  models?: string[]
  unrestricted?: boolean
}

export interface TokenPlanPurchaseResult {
  subscription: TokenPlanSubscription
  key: string
}

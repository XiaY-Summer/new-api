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
  window1_seconds: number
  window1_tokens: number
  window2_seconds: number
  window2_tokens: number
  metering_mode: TokenPlanMeteringMode
  model_limits: string
  enabled: boolean
  sort_order: number
}

export interface TokenPlanPayload {
  plan: TokenPlanAdminRecord
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
    window1_seconds: 5 * 60 * 60,
    window1_tokens: 1_000_000,
    window2_seconds: 7 * 24 * 60 * 60,
    window2_tokens: 3_000_000,
    metering_mode: 'weighted',
    model_limits: '',
    enabled: true,
    sort_order: 0,
  }
}

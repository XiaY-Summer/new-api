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
import { api } from '@/lib/api'

import type {
  ApiResponse,
  TokenPlan,
  TokenPlanPurchaseResult,
  TokenPlanSelf,
} from './types'

export async function getTokenPlanPlans(): Promise<ApiResponse<TokenPlan[]>> {
  const res = await api.get('/api/token-plan/plans')
  return res.data
}

export async function getTokenPlanSelf(): Promise<ApiResponse<TokenPlanSelf>> {
  const res = await api.get('/api/token-plan/self')
  return res.data
}

export async function purchaseTokenPlan(
  planId: number
): Promise<ApiResponse<TokenPlanPurchaseResult>> {
  const res = await api.post('/api/token-plan/purchase', { plan_id: planId })
  return res.data
}

export async function resetTokenPlanKey(): Promise<
  ApiResponse<{ key: string }>
> {
  const res = await api.post('/api/token-plan/key/reset')
  return res.data
}

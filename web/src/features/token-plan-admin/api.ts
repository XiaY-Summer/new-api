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
  TokenPlanAdminRecord,
  TokenPlanSubscriber,
} from './types'

export async function getAdminTokenPlans(): Promise<
  ApiResponse<TokenPlanAdminRecord[]>
> {
  const res = await api.get('/api/token-plan/admin/plans')
  return res.data
}

export async function createTokenPlan(
  plan: TokenPlanAdminRecord
): Promise<ApiResponse<TokenPlanAdminRecord>> {
  const res = await api.post('/api/token-plan/admin/plans', { plan })
  return res.data
}

export async function updateTokenPlan(
  plan: TokenPlanAdminRecord
): Promise<ApiResponse<TokenPlanAdminRecord>> {
  const res = await api.put(`/api/token-plan/admin/plans/${plan.id}`, { plan })
  return res.data
}

/** 删除套餐会同时取消该套餐所有生效中的订阅，返回被取消的数量。 */
export async function deleteTokenPlan(
  id: number
): Promise<ApiResponse<{ cancelled: number }>> {
  const res = await api.delete(`/api/token-plan/admin/plans/${id}`)
  return res.data
}

/** 分组 -> 该分组下已启用的模型列表。 */
export async function getTokenPlanGroupModels(): Promise<
  ApiResponse<Record<string, string[]>>
> {
  const res = await api.get('/api/token-plan/admin/group-models')
  return res.data
}

export async function getTokenPlanSubscribers(): Promise<
  ApiResponse<TokenPlanSubscriber[]>
> {
  const res = await api.get('/api/token-plan/admin/subscribers')
  return res.data
}

/** userId 为空表示重置全体用户。 */
export async function resetTokenPlanUsage(
  userId?: number
): Promise<ApiResponse<{ affected: number }>> {
  const res = await api.post('/api/token-plan/admin/reset-usage', {
    user_id: userId ?? 0,
  })
  return res.data
}

/** 把套餐当前的节流参数推送到该套餐所有生效中的订阅。 */
export async function syncTokenPlanThrottle(
  id: number
): Promise<ApiResponse<{ affected: number }>> {
  const res = await api.post(`/api/token-plan/admin/plans/${id}/sync-throttle`)
  return res.data
}

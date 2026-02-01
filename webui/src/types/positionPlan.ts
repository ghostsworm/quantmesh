// 倉位目標计划類型定义

// 计划状態
export type PlanStatus = 'pending' | 'in_progress' | 'completed' | 'cancelled'

// 计划方向
export type PlanDirection = 'reduce' | 'increase'

// 倉位计划
export interface PositionPlan {
  id: number
  exchange: string
  symbol: string
  strategyId: string
  targetAmountUsdt: number
  direction: PlanDirection
  status: PlanStatus
  initialAmount: number
  currentAmount: number
  notifyOnComplete: boolean
  autoAdjustLimit: boolean
  originalLimit: number
  createdAt: string
  updatedAt: string
  completedAt?: string
}

// 創建计划请求
export interface CreatePositionPlanRequest {
  exchange: string
  symbol: string
  strategyId?: string
  targetAmountUsdt: number
  notifyOnComplete: boolean
  autoAdjustLimit: boolean
}

// 更新计划请求
export interface UpdatePositionPlanRequest {
  targetAmountUsdt?: number
  notifyOnComplete?: boolean
}

// 取消计划请求
export interface CancelPositionPlanRequest {
  restoreLimit: boolean
}

// 獲取计划列表响应
export interface GetPositionPlansResponse {
  success: boolean
  plans: PositionPlan[]
  error?: string
}

// 獲取單個计划响应
export interface GetPositionPlanResponse {
  success: boolean
  plan: PositionPlan
  error?: string
}

// 創建计划响应
export interface CreatePositionPlanResponse {
  success: boolean
  plan?: PositionPlan
  message: string
  current?: number
  target?: number
  error?: string
}

// 更新/取消计划响应
export interface UpdatePositionPlanResponse {
  success: boolean
  plan?: PositionPlan
  message: string
  error?: string
}

// 检查倉位响应
export interface CheckPositionPlanResponse {
  success: boolean
  exchange: string
  symbol: string
  currentAmount: number
  hasActivePlan: boolean
  plan?: PositionPlan
  targetAmount?: number
  direction?: PlanDirection
  reached?: boolean
  error?: string
}

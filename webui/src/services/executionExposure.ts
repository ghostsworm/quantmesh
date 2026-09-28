import type { PositionStatus } from './api'

export type ExecutionExposure = {
  ready: boolean
  reason_code: string
  opening_available: boolean
  new_lot_available: boolean
  position_quantity: number
  pending_quantity: number
  projected_quantity: number
  projected_notional: number
  layers: number
  limits: { quantity: number; notional: number; layers: number }
  mark: number
  mark_at: string
}

export type ExecutionPositionStatus = PositionStatus & { execution_exposure?: ExecutionExposure; valuation_available?: boolean }

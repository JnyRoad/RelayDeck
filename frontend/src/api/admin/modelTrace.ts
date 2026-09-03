import { apiClient } from '../client'
import type { PaginatedResponse } from '@/types'

export type ModelTraceOutcome = 'succeeded' | 'failed' | 'blocked' | 'client_cancelled' | 'partial'
export type ModelTraceCaptureStatus = 'complete' | 'truncated' | 'redacted' | 'not_applicable' | 'failed'

export interface ModelTraceConfig {
  enabled: boolean
  payload_capture_enabled: boolean
  auto_cleanup_enabled: boolean
  retention_days: number
}

export interface ModelTraceSummary {
  trace_id: string
  request_id: string
  user_id?: number
  api_key_id?: number
  group_id?: number
  account_id?: number
  route: string
  protocol: string
  requested_model: string
  upstream_model: string
  response_model: string
  outcome: ModelTraceOutcome
  status_code?: number
  stream: boolean
  duration_ms?: number
  first_byte_ms?: number
  request_capture_status: ModelTraceCaptureStatus
  response_capture_status: ModelTraceCaptureStatus
  request_bytes: number
  response_bytes: number
  expires_at: string
  created_at: string
  completed_at?: string
}

export interface ModelTracePayload {
  kind: 'client_request' | 'client_response' | 'error_response' | 'upstream_attempt'
  attempt_no: number
  capture_status: ModelTraceCaptureStatus
  content_type: string
  original_bytes: number
  stored_bytes: number
  sha256: string
  created_at: string
  content?: string
  content_status: 'available' | 'unavailable' | 'not_captured'
}

export interface ModelTraceDetail {
  trace: ModelTraceSummary
  payloads: ModelTracePayload[]
}

export interface ModelTraceQueryParams {
  page?: number
  page_size?: number
  user_id?: number
  api_key_id?: number
  group_id?: number
  account_id?: number
  trace_id?: string
  request_id?: string
  route?: string
  requested_model?: string
  protocol?: string
  outcome?: ModelTraceOutcome
  capture_status?: ModelTraceCaptureStatus
  start_time?: string
  end_time?: string
}

export interface ModelTraceCleanupPreview {
  expired_traces: number
  expired_payloads: number
  stored_bytes: number
  cutoff_at: string
}

/** 查询模型调用的轻量索引，列表端不请求任何正文内容。 */
export async function list(params: ModelTraceQueryParams): Promise<PaginatedResponse<ModelTraceSummary>> {
  const { data } = await apiClient.get<PaginatedResponse<ModelTraceSummary>>('/admin/model-traces', { params })
  return data
}

/** 按 trace ID 请求一条调用头和正文元数据，不读取或解密正文。 */
export async function getDetail(traceID: string): Promise<ModelTraceDetail> {
  const { data } = await apiClient.get<ModelTraceDetail>(`/admin/model-traces/${encodeURIComponent(traceID)}`)
  return data
}

/** 仅在管理员打开对应页签后请求一种已脱敏的正文。 */
export async function getPayload(traceID: string, kind: ModelTracePayload['kind'], attemptNo = 0): Promise<ModelTracePayload> {
  const { data } = await apiClient.get<ModelTracePayload>(`/admin/model-traces/${encodeURIComponent(traceID)}/payloads/${encodeURIComponent(kind)}`, {
    params: { attempt_no: attemptNo },
  })
  return data
}

/** 读取当前的追踪、正文采集和自动清理策略。 */
export async function getConfig(): Promise<ModelTraceConfig> {
  const { data } = await apiClient.get<ModelTraceConfig>('/admin/model-traces/config')
  return data
}

/** 保存完整策略快照，防止隐式改变任一安全开关。 */
export async function updateConfig(config: ModelTraceConfig): Promise<ModelTraceConfig> {
  const { data } = await apiClient.put<ModelTraceConfig>('/admin/model-traces/config', config)
  return data
}

/** 预览已到期调用与正文占用，不执行删除。 */
export async function previewCleanup(): Promise<ModelTraceCleanupPreview> {
  const { data } = await apiClient.get<ModelTraceCleanupPreview>('/admin/model-traces/cleanup-preview')
  return data
}

/** 执行管理员已确认的到期调用清理。 */
export async function runCleanup(): Promise<{ deleted_traces: number; deleted_payloads: number; deleted_bytes: number }> {
  const { data } = await apiClient.post<{ deleted_traces: number; deleted_payloads: number; deleted_bytes: number }>('/admin/model-traces/cleanup')
  return data
}

export const modelTraceAPI = { list, getDetail, getPayload, getConfig, updateConfig, previewCleanup, runCleanup }

export default modelTraceAPI

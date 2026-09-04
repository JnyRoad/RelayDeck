import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelTraceDetailDialog from '../ModelTraceDetailDialog.vue'

const { getConversation, getPayload, recordAccessEvent } = vi.hoisted(() => ({
  getConversation: vi.fn(),
  getPayload: vi.fn(),
  recordAccessEvent: vi.fn(),
}))

vi.mock('@/api/admin/modelTrace', () => ({
  modelTraceAPI: { getConversation, getPayload, recordAccessEvent },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string, values?: Record<string, unknown>) => `${key}${values ? JSON.stringify(values) : ''}` }),
}))

describe('ModelTraceDetailDialog', () => {
  beforeEach(() => {
    getConversation.mockReset().mockResolvedValue({
      current_trace_id: 'trace-current',
      linked: true,
      link_source: 'session_id',
      turns: [
        { trace: { trace_id: 'trace-first', created_at: '2026-09-03T12:00:00Z' }, payloads: [{ kind: 'client_request', attempt_no: 0, content_status: 'available' }, { kind: 'client_response', attempt_no: 0, content_status: 'available' }] },
        {
          trace: { trace_id: 'trace-current', route: '/v1/responses', requested_model: 'gpt-test', created_at: '2026-09-03T12:01:00Z' },
          attempts: [
            { attempt_no: 1, account_snapshot: 'account-first', upstream_route: 'https://upstream.example/first', upstream_model: 'first-model', outcome: 'failed', status_code: 502 },
            { attempt_no: 2, account_snapshot: 'account-second', upstream_route: 'https://upstream.example/second', upstream_model: 'second-model', outcome: 'succeeded', status_code: 200 },
          ],
          payloads: [
            { kind: 'client_request', attempt_no: 0, content_status: 'available' },
            { kind: 'client_response', attempt_no: 0, content_status: 'available' },
            { kind: 'upstream_request', attempt_no: 1, content_status: 'available', capture_status: 'complete', original_bytes: 120, stored_bytes: 112, sha256: 'attempt-one-hash' },
            { kind: 'upstream_error', attempt_no: 1, content_status: 'available' },
            { kind: 'upstream_request', attempt_no: 2, content_status: 'available' },
            { kind: 'upstream_response', attempt_no: 2, content_status: 'available' },
          ],
        },
        { trace: { trace_id: 'trace-last', created_at: '2026-09-03T12:02:00Z' }, payloads: [{ kind: 'client_request', attempt_no: 0, content_status: 'available' }, { kind: 'client_response', attempt_no: 0, content_status: 'available' }] },
      ],
    })
    getPayload.mockReset().mockImplementation(async (traceID: string, kind: string) => ({
      kind,
      attempt_no: 0,
      content_status: 'available',
      content: `${traceID}-${kind}`,
    }))
    recordAccessEvent.mockReset().mockResolvedValue(undefined)
  })

  const mountDialog = (traceId: string) => mount(ModelTraceDetailDialog, {
    props: { show: true, traceId },
    global: {
      stubs: {
        BaseDialog: { props: ['show', 'title'], template: '<div v-if="show"><slot /></div>' },
      },
    },
  })

  it('renders an independently scrollable, current-turn-focused conversation replay', async () => {
    const wrapper = mountDialog('trace-current')
    await flushPromises()

    expect(getConversation).toHaveBeenCalledWith('trace-current')
    expect(wrapper.findAll('[data-testid^="trace-chat-turn-"]')).toHaveLength(3)
    expect(wrapper.get('[data-testid="trace-chat-turn-trace-current"]').classes()).toContain('trace-current-turn')
    expect(wrapper.get('[data-testid="model-trace-chat-scroll"]').classes()).toContain('overflow-y-auto')
    expect(wrapper.text()).toContain('trace-first-client_request')
    expect(wrapper.text()).toContain('trace-last-client_response')
  })

  it('labels an unlinked record instead of inferring a conversation from identity', async () => {
    getConversation.mockResolvedValueOnce({
      current_trace_id: 'trace-alone',
      linked: false,
      link_source: '',
      turns: [{ trace: { trace_id: 'trace-alone' }, payloads: [] }],
    })
    const wrapper = mountDialog('trace-alone')
    await flushPromises()

    expect(wrapper.get('[data-testid="model-trace-unlinked"]').text()).toContain('admin.modelTrace.detail.unlinked')
    expect(wrapper.findAll('[data-testid^="trace-chat-turn-"]')).toHaveLength(1)
  })

  it('renders retry attempts in raw order and only loads an upstream body when asked', async () => {
    const wrapper = mountDialog('trace-current')
    await flushPromises()
    getPayload.mockClear()

    await wrapper.get('[data-testid="model-trace-view-raw"]').trigger('click')
    const raw = wrapper.get('[data-testid="model-trace-raw-chain"]')
    expect(raw.text().indexOf('account-first')).toBeLessThan(raw.text().indexOf('account-second'))
    expect(raw.text()).toContain('attempt-one-hash')
    expect(getPayload).not.toHaveBeenCalled()

    await raw.findAll('button').find((button) => button.text().includes('admin.modelTrace.detail.loadBody'))!.trigger('click')
    await flushPromises()
    expect(getPayload).toHaveBeenCalledWith('trace-current', 'upstream_request', 1)
  })
})

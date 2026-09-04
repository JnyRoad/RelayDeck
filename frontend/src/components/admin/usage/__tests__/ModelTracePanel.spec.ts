import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelTracePanel from '../ModelTracePanel.vue'

const { list, getDetail, getPayload, getConfig, updateConfig, previewCleanup, runCleanup } = vi.hoisted(() => ({
  list: vi.fn(),
  getDetail: vi.fn(),
  getPayload: vi.fn(),
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
  previewCleanup: vi.fn(),
  runCleanup: vi.fn(),
}))

vi.mock('@/api/admin/modelTrace', () => ({
  modelTraceAPI: { list, getDetail, getPayload, getConfig, updateConfig, previewCleanup, runCleanup },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('ModelTracePanel', () => {
  beforeEach(() => {
    list.mockReset().mockResolvedValue({ items: [{
      trace_id: 'trace-list', route: '/v1/chat/completions', requested_model: 'gpt-test',
      outcome: 'succeeded', status_code: 200, created_at: '2026-09-03T12:00:00Z',
      request_capture_status: 'redacted', response_capture_status: 'redacted',
    }], total: 1, page: 1, page_size: 20, pages: 1 })
    getConfig.mockReset().mockResolvedValue({ enabled: false, payload_capture_enabled: false, auto_cleanup_enabled: false, retention_days: 7 })
    getDetail.mockReset().mockResolvedValue({
      trace: { trace_id: 'trace-list' },
      payloads: [{ kind: 'client_request', attempt_no: 0, content_status: 'available' }],
    })
	getPayload.mockReset().mockResolvedValue({ kind: 'client_request', attempt_no: 0, content_status: 'available', content: '{"prompt":"[REDACTED]"}', ciphertext: 'must-not-render' })
    updateConfig.mockReset()
    previewCleanup.mockReset()
    runCleanup.mockReset()
  })

  it('loads lightweight rows, then reveals only safe detail content on demand', async () => {
    const wrapper = mount(ModelTracePanel)
    await flushPromises()

    expect(list).toHaveBeenCalledWith(expect.objectContaining({ page: 1, page_size: 20 }))
    expect(wrapper.text()).toContain('gpt-test')

    await wrapper.get('[data-testid="model-trace-row-trace-list"]').trigger('click')
    await flushPromises()
		await wrapper.get('[data-testid="model-trace-payload-client_request-0"]').trigger('click')
		await flushPromises()

    expect(getDetail).toHaveBeenCalledWith('trace-list')
		expect(getPayload).toHaveBeenCalledWith('trace-list', 'client_request', 0)
    expect(wrapper.text()).toContain('[REDACTED]')
    expect(wrapper.text()).not.toContain('must-not-render')
  })

  it('persists the retention and auto-cleanup switches as one explicit config update', async () => {
    updateConfig.mockResolvedValue({ enabled: true, payload_capture_enabled: true, auto_cleanup_enabled: false, retention_days: 14 })
    const wrapper = mount(ModelTracePanel)
    await flushPromises()

    await wrapper.get('[data-testid="model-trace-enabled"]').setValue(true)
    await wrapper.get('[data-testid="model-trace-payload-enabled"]').setValue(true)
    await wrapper.get('[data-testid="model-trace-auto-cleanup"]').setValue(false)
    await wrapper.get('[data-testid="model-trace-retention-days"]').setValue(14)
    await wrapper.get('[data-testid="model-trace-save"]').trigger('click')

    expect(updateConfig).toHaveBeenCalledWith({ enabled: true, payload_capture_enabled: true, auto_cleanup_enabled: false, retention_days: 14 })
  })

  it('uses a native button to make opening a trace keyboard operable', async () => {
    const wrapper = mount(ModelTracePanel)
    await flushPromises()

    expect(wrapper.get('[data-testid="model-trace-row-trace-list"]').element.tagName).toBe('BUTTON')
  })

  it('ignores an older list response after a newer search completes', async () => {
    let resolveFirst: (value: any) => void = () => undefined
    const first = new Promise<any>((resolve) => { resolveFirst = resolve })
    list.mockReset().mockReturnValueOnce(first).mockResolvedValueOnce({
      items: [{ trace_id: 'trace-new', route: '/v1/responses', requested_model: 'new-model', outcome: 'succeeded', created_at: '2026-09-03T12:00:00Z', request_capture_status: 'redacted', response_capture_status: 'redacted' }],
      total: 1, page: 1, page_size: 20, pages: 1,
    })
    const wrapper = mount(ModelTracePanel)
    await flushPromises()

    await wrapper.get('button.btn.btn-secondary').trigger('click')
    await flushPromises()
		resolveFirst({ items: [{ trace_id: 'trace-old', route: '/v1/responses', requested_model: 'old-model', outcome: 'succeeded', created_at: '2026-09-03T12:00:00Z', request_capture_status: 'redacted', response_capture_status: 'redacted' }], total: 1, page: 1, page_size: 20, pages: 1 })
    await flushPromises()

    expect(wrapper.text()).toContain('new-model')
    expect(wrapper.text()).not.toContain('old-model')
  })

  it('ignores an older detail response after the same trace is reopened', async () => {
    let resolveFirst: (value: any) => void = () => undefined
    const first = new Promise<any>((resolve) => { resolveFirst = resolve })
    getDetail.mockReset()
      .mockReturnValueOnce(first)
      .mockResolvedValueOnce({ trace: { trace_id: 'trace-new-detail' }, payloads: [] })
    const wrapper = mount(ModelTracePanel)
    await flushPromises()

    await wrapper.get('[data-testid="model-trace-row-trace-list"]').trigger('click')
    await wrapper.get('[data-testid="model-trace-row-trace-list"]').trigger('click')
    await flushPromises()
		resolveFirst({ trace: { trace_id: 'trace-old-detail' }, payloads: [] })
    await flushPromises()

    expect(wrapper.text()).toContain('trace-new-detail')
    expect(wrapper.text()).not.toContain('trace-old-detail')
  })

  it('does not overwrite a saved config with a pre-save config read', async () => {
    let resolveSave: (value: any) => void = () => undefined
    let resolveStaleConfig: (value: any) => void = () => undefined
    const save = new Promise<any>((resolve) => { resolveSave = resolve })
    const staleConfig = new Promise<any>((resolve) => { resolveStaleConfig = resolve })
    getConfig.mockReset()
      .mockResolvedValueOnce({ enabled: false, payload_capture_enabled: false, auto_cleanup_enabled: false, retention_days: 7 })
      .mockReturnValueOnce(staleConfig)
    updateConfig.mockReset().mockReturnValueOnce(save)
    const wrapper = mount(ModelTracePanel)
    await flushPromises()

    await wrapper.get('[data-testid="model-trace-enabled"]').setValue(true)
    await wrapper.get('[data-testid="model-trace-retention-days"]').setValue(14)
    await wrapper.get('[data-testid="model-trace-save"]').trigger('click')
    await wrapper.get('button.btn.btn-secondary').trigger('click')
    await flushPromises()
    resolveSave({ enabled: true, payload_capture_enabled: false, auto_cleanup_enabled: true, retention_days: 14 })
    await flushPromises()
    resolveStaleConfig({ enabled: false, payload_capture_enabled: false, auto_cleanup_enabled: false, retention_days: 7 })
    await flushPromises()

    expect((wrapper.get('[data-testid="model-trace-enabled"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-testid="model-trace-auto-cleanup"]').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-testid="model-trace-retention-days"]').element as HTMLInputElement).value).toBe('14')
  })
})

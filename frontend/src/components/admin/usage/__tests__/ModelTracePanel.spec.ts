import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import ModelTracePanel from '../ModelTracePanel.vue'

const { list, getConfig, updateConfig, previewCleanup, runCleanup } = vi.hoisted(() => ({
  list: vi.fn(),
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
  previewCleanup: vi.fn(),
  runCleanup: vi.fn(),
}))

vi.mock('@/api/admin/modelTrace', () => ({
  modelTraceAPI: { list, getConfig, updateConfig, previewCleanup, runCleanup },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('ModelTracePanel', () => {
  beforeEach(() => {
    list.mockReset().mockResolvedValue({ items: [{
      trace_id: 'trace-list', route: '/v1/chat/completions', requested_model: 'gpt-test',
      user_snapshot: 'dingrui@szyuto.com', api_key_snapshot: 'sk-user-key',
      outcome: 'succeeded', status_code: 200, created_at: '2026-09-03T12:00:00Z',
      request_capture_status: 'redacted', response_capture_status: 'redacted',
    }], total: 1, page: 1, page_size: 20, pages: 1 })
    getConfig.mockReset().mockResolvedValue({ enabled: false, payload_capture_enabled: false, auto_cleanup_enabled: false, retention_days: 7 })
    updateConfig.mockReset()
    previewCleanup.mockReset()
    runCleanup.mockReset()
  })

  it('filters lightweight rows by owner or key and opens the independent conversation dialog', async () => {
    const wrapper = mount(ModelTracePanel, {
      global: { stubs: { ModelTraceDetailDialog: { props: ['show', 'traceId'], template: '<div data-testid="trace-dialog" :data-open="String(show)" :data-trace-id="traceId" />' } } },
    })
    await flushPromises()

    expect(list).toHaveBeenCalledWith(expect.objectContaining({ page: 1, page_size: 20 }))
    expect(wrapper.text()).toContain('gpt-test')
    expect(wrapper.text()).toContain('dingrui@szyuto.com')
    expect(wrapper.text()).toContain('sk-user-key')

    await wrapper.get('[data-testid="model-trace-filter-user"]').setValue('dingrui')
    await wrapper.get('[data-testid="model-trace-filter-key"]').setValue('sk-user')
    await wrapper.get('[data-testid="model-trace-search"]').trigger('click')
    await flushPromises()
    expect(list).toHaveBeenLastCalledWith(expect.objectContaining({ user: 'dingrui', api_key: 'sk-user' }))

    await wrapper.get('[data-testid="model-trace-row-trace-list"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="trace-dialog"]').attributes('data-open')).toBe('true')
    expect(wrapper.get('[data-testid="trace-dialog"]').attributes('data-trace-id')).toBe('trace-list')
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

  it('allows at most 365 retention days and blocks an out-of-range save in the browser', async () => {
    const wrapper = mount(ModelTracePanel)
    await flushPromises()

    const retention = wrapper.get('[data-testid="model-trace-retention-days"]')
    expect(retention.attributes('min')).toBe('1')
    expect(retention.attributes('max')).toBe('365')
    await retention.setValue(366)
    await wrapper.get('[data-testid="model-trace-save"]').trigger('click')

    expect(updateConfig).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('admin.modelTrace.errors.retention')
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

    await wrapper.get('[data-testid="model-trace-search"]').trigger('click')
    await flushPromises()
		resolveFirst({ items: [{ trace_id: 'trace-old', route: '/v1/responses', requested_model: 'old-model', outcome: 'succeeded', created_at: '2026-09-03T12:00:00Z', request_capture_status: 'redacted', response_capture_status: 'redacted' }], total: 1, page: 1, page_size: 20, pages: 1 })
    await flushPromises()

    expect(wrapper.text()).toContain('new-model')
    expect(wrapper.text()).not.toContain('old-model')
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
    await wrapper.get('[data-testid="model-trace-search"]').trigger('click')
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

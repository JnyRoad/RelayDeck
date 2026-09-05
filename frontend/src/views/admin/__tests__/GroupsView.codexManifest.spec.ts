import { flushPromises, shallowMount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import GroupsView from '../GroupsView.vue'
import { adminAPI } from '@/api/admin'
import type { AdminGroup } from '@/types'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key })
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() })
}))
vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({ isCurrentStep: () => false })
}))
vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: {
      list: vi.fn(),
      update: vi.fn(),
      getLiveCapability: vi.fn().mockResolvedValue({ supported: false }),
      getModelsListCandidates: vi.fn().mockResolvedValue([]),
      getUsageSummary: vi.fn().mockResolvedValue([]),
      getCapacitySummary: vi.fn().mockResolvedValue([])
    },
    accounts: {
      getById: vi.fn().mockResolvedValue({ id: 5, name: 'oauth-five' }),
      list: vi.fn().mockResolvedValue({
        items: [{ id: 6, name: 'oauth-six' }],
        total: 1, page: 1, page_size: 20, pages: 1
      })
    }
  }
}))

const group: AdminGroup = {
  id: 7, name: 'OpenAI group', description: null, platform: 'openai',
  rate_multiplier: 1, is_exclusive: false, status: 'active',
  subscription_type: 'standard', daily_limit_usd: null,
  weekly_limit_usd: null, monthly_limit_usd: null,
  long_context_pricing_enabled: true, force_openai_fast: false,
  free_openai_fast: false, model_pricing: [], allow_image_generation: false,
  allow_batch_image_generation: false, image_rate_independent: false,
  image_rate_multiplier: 1, batch_image_discount_multiplier: 0.5,
  batch_image_hold_multiplier: 0.6, image_price_1k: null,
  image_price_2k: null, image_price_4k: null, video_rate_independent: false,
  video_rate_multiplier: 1, video_price_480p: null, video_price_720p: null,
  video_price_1080p: null, web_search_price_per_call: null,
  search_price_per_1k: null, audio_realtime_price_per_min: null,
  audio_tts_price_per_million_chars: null, audio_stt_price_per_hour: null,
  peak_rate_enabled: false, peak_start: '', peak_end: '', peak_rate_multiplier: 1,
  claude_code_only: false, fallback_group_id: null,
  fallback_group_id_on_invalid_request: null, allow_live: false,
  require_oauth_only: false, require_privacy_set: false,
  profit_control_enabled: false, profit_min_margin: 0, profit_safety_buffer: 0,
  model_routing: null, model_routing_enabled: false, mcp_xml_inject: true,
  sort_order: 0, created_at: '2026-09-05T00:00:00Z', updated_at: '2026-09-05T00:00:00Z',
  codex_models_manifest_config: {
    enabled: false, account_ids: [5], fallback_to_scheduler: false
  }
}

describe('GroupsView Codex manifest editing', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    localStorage.clear()
    vi.mocked(adminAPI.groups.list).mockResolvedValue({
      items: [group], total: 1, page: 1, page_size: 20, pages: 1
    })
    vi.mocked(adminAPI.groups.update).mockResolvedValue(group)
  })

  it('reactively preserves successive manifest edits through the real group dialog', async () => {
    // Keep the real parent binding, dialog, field, and validation. Only the
    // unrelated layout/table rendering and external API requests are replaced.
    const wrapper = shallowMount(GroupsView, {
      attachTo: document.body,
      global: {
        renderStubDefaultSlot: true,
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          TablePageLayout: false,
          DataTable: {
            props: ['data'],
            template: '<div><div v-for="row in data" :key="row.id"><slot name="cell-actions" :row="row" /></div></div>'
          },
          BaseDialog: false,
          CodexManifestAccountsField: false,
          ReasoningEffortPolicyFields: false,
          teleport: true
        }
      }
    })
    try {
      await flushPromises()
      const edit = wrapper.findAll('button').find(button => button.text() === 'common.edit')!
      await edit.trigger('click')
      await flushPromises()

      expect(wrapper.find('[data-testid="codex-manifest-search"]').exists()).toBe(false)
      await wrapper.get('[data-testid="codex-manifest-toggle"]').trigger('click')
      expect(wrapper.find('[data-testid="codex-manifest-search"]').exists()).toBe(true)
      expect(wrapper.get('[data-testid="codex-manifest-selected-tags"]').text()).toContain('oauth-five')

      const fallback = wrapper.get('[data-testid="codex-manifest-fallback-toggle"]')
      await fallback.trigger('click')
      expect(fallback.classes()).toContain('bg-primary-500')
      await wrapper.get('[aria-label="remove account 5"]').trigger('click')
      expect(wrapper.find('[data-testid="codex-manifest-selected-tags"]').exists()).toBe(false)

      const search = wrapper.get('[data-testid="codex-manifest-search"]')
      await search.trigger('focus')
      await search.setValue('oauth-six')
      await vi.waitFor(() => {
        expect(wrapper.get('[data-testid="codex-manifest-dropdown"]').text()).toContain('oauth-six')
      })
      await wrapper.get('[data-testid="codex-manifest-dropdown"] button').trigger('click')
      expect(wrapper.get('[data-testid="codex-manifest-selected-tags"]').text()).toContain('oauth-six')
      expect(fallback.classes()).toContain('bg-primary-500')
      await wrapper.get('#edit-group-form').trigger('submit')
      await flushPromises()
      expect(adminAPI.groups.update).toHaveBeenCalledWith(7, expect.objectContaining({
        codex_models_manifest_config: {
          enabled: true, account_ids: [6], fallback_to_scheduler: true
        }
      }))

      // Reopening must hydrate the saved API response, not leak unsaved edits.
      await edit.trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-testid="codex-manifest-search"]').exists()).toBe(false)
      await wrapper.get('[data-testid="codex-manifest-toggle"]').trigger('click')
      expect(wrapper.get('[data-testid="codex-manifest-selected-tags"]').text()).toContain('oauth-five')
      expect(wrapper.get('[data-testid="codex-manifest-fallback-toggle"]').classes()).not.toContain('bg-primary-500')
    } finally {
      wrapper.unmount()
    }
  })
})

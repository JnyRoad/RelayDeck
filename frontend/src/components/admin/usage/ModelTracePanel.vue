<template>
  <section class="space-y-4 p-4" data-testid="model-trace-panel">
    <div class="rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-700 dark:bg-dark-800/60">
      <div class="mb-3 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">{{ t('admin.modelTrace.settings.title') }}</h3>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.modelTrace.settings.hint') }}</p>
        </div>
        <button data-testid="model-trace-save" class="btn btn-primary" :disabled="saving" @click="saveConfig">
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
      <div class="grid gap-3 md:grid-cols-4">
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
          <input v-model="config.enabled" data-testid="model-trace-enabled" type="checkbox" class="checkbox checkbox-sm" />
          {{ t('admin.modelTrace.settings.enabled') }}
        </label>
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
          <input v-model="config.payload_capture_enabled" data-testid="model-trace-payload-enabled" type="checkbox" class="checkbox checkbox-sm" :disabled="!config.enabled" />
          {{ t('admin.modelTrace.settings.payload') }}
        </label>
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
          <input v-model="config.auto_cleanup_enabled" data-testid="model-trace-auto-cleanup" type="checkbox" class="checkbox checkbox-sm" />
          {{ t('admin.modelTrace.settings.autoCleanup') }}
        </label>
        <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
          <span>{{ t('admin.modelTrace.settings.retention') }}</span>
          <input v-model.number="config.retention_days" data-testid="model-trace-retention-days" type="number" min="1" max="365" class="input w-20 px-2 py-1" />
        </label>
      </div>
    </div>

    <div class="flex flex-wrap gap-2">
      <input v-model.trim="filters.request_id" class="input min-w-48 flex-1" :placeholder="t('admin.modelTrace.filters.requestId')" @keyup.enter="applyFilters" />
      <input v-model.trim="filters.user" data-testid="model-trace-filter-user" class="input min-w-40 flex-1" :placeholder="t('admin.modelTrace.filters.user')" @keyup.enter="applyFilters" />
      <input v-model.trim="filters.api_key" data-testid="model-trace-filter-key" class="input min-w-40 flex-1" :placeholder="t('admin.modelTrace.filters.apiKey')" @keyup.enter="applyFilters" />
      <input v-model.trim="filters.requested_model" class="input min-w-40 flex-1" :placeholder="t('admin.modelTrace.filters.model')" @keyup.enter="applyFilters" />
      <select v-model="filters.outcome" class="input w-40" @change="applyFilters">
        <option value="">{{ t('admin.modelTrace.filters.allOutcomes') }}</option>
        <option v-for="outcome in outcomes" :key="outcome" :value="outcome">{{ outcome }}</option>
      </select>
      <button data-testid="model-trace-search" class="btn btn-secondary" @click="applyFilters">{{ t('common.search') }}</button>
      <button class="btn btn-secondary" :disabled="previewing" @click="loadCleanupPreview">{{ t('admin.modelTrace.actions.previewCleanup') }}</button>
      <button class="btn btn-danger" :disabled="cleaning || !cleanupPreview?.expired_traces" @click="confirmCleanup">{{ t('admin.modelTrace.actions.cleanup') }}</button>
    </div>

    <p v-if="cleanupPreview" class="rounded-lg bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:bg-amber-950/30 dark:text-amber-200">
      {{ t('admin.modelTrace.cleanup.preview', { traces: cleanupPreview.expired_traces, attempts: cleanupPreview.expired_attempts, payloads: cleanupPreview.expired_payloads, bytes: formatBytes(cleanupPreview.stored_bytes) }) }}
    </p>
    <p v-if="errorMessage" class="rounded-lg bg-red-50 px-3 py-2 text-sm text-red-700 dark:bg-red-950/30 dark:text-red-300">{{ errorMessage }}</p>

    <div class="overflow-x-auto rounded-xl border border-gray-200 dark:border-dark-700">
      <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
        <thead class="bg-gray-50 text-left text-xs font-medium uppercase text-gray-500 dark:bg-dark-800 dark:text-gray-400">
          <tr><th class="px-3 py-2">{{ t('admin.modelTrace.columns.time') }}</th><th class="px-3 py-2">{{ t('admin.modelTrace.columns.user') }}</th><th class="px-3 py-2">{{ t('admin.modelTrace.columns.apiKey') }}</th><th class="px-3 py-2">{{ t('admin.modelTrace.columns.route') }}</th><th class="px-3 py-2">{{ t('admin.modelTrace.columns.model') }}</th><th class="px-3 py-2">{{ t('admin.modelTrace.columns.result') }}</th><th class="px-3 py-2">{{ t('admin.modelTrace.columns.duration') }}</th></tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
          <tr v-if="loading"><td colspan="7" class="px-3 py-8 text-center text-gray-500">{{ t('common.loading') }}</td></tr>
          <tr v-else-if="items.length === 0"><td colspan="7" class="px-3 py-8 text-center text-gray-500">{{ t('admin.modelTrace.empty') }}</td></tr>
          <tr v-for="item in items" :key="item.trace_id" class="hover:bg-primary-50 dark:hover:bg-primary-950/20">
            <td class="whitespace-nowrap px-3 py-2 text-gray-600 dark:text-gray-300"><button :data-testid="`model-trace-row-${item.trace_id}`" class="rounded text-left underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500" @click="openDetail(item.trace_id)">{{ formatTime(item.created_at) }}</button></td>
            <td class="max-w-40 truncate px-3 py-2">{{ item.user_snapshot || '—' }}</td>
            <td class="max-w-40 truncate px-3 py-2 font-mono text-xs">{{ item.api_key_snapshot || '—' }}</td>
            <td class="max-w-56 truncate px-3 py-2 font-mono text-xs">{{ item.route }}</td>
            <td class="max-w-48 truncate px-3 py-2">{{ item.requested_model || item.response_model || '—' }}</td>
            <td class="px-3 py-2"><span class="rounded-full bg-gray-100 px-2 py-0.5 text-xs dark:bg-dark-700">{{ item.outcome }} · {{ item.status_code ?? '—' }}</span></td>
            <td class="px-3 py-2">{{ item.duration_ms == null ? '—' : `${item.duration_ms} ms` }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="flex items-center justify-between text-sm text-gray-500">
      <span>{{ t('admin.modelTrace.pagination.total', { total }) }}</span>
      <div class="flex gap-2"><button class="btn btn-secondary btn-sm" :disabled="page <= 1 || loading" @click="changePage(page - 1)">{{ t('common.back') }}</button><button class="btn btn-secondary btn-sm" :disabled="page * pageSize >= total || loading" @click="changePage(page + 1)">{{ t('common.next') }}</button></div>
    </div>

    <ModelTraceDetailDialog :show="selectedTraceId !== null" :trace-id="selectedTraceId" @close="selectedTraceId = null" />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import ModelTraceDetailDialog from './ModelTraceDetailDialog.vue'
import { modelTraceAPI } from '@/api/admin/modelTrace'
import type { ModelTraceConfig, ModelTraceOutcome, ModelTraceQueryParams, ModelTraceSummary, ModelTraceCleanupPreview } from '@/api/admin/modelTrace'

const { t } = useI18n()
const page = ref(1)
const pageSize = 20
const total = ref(0)
const items = ref<ModelTraceSummary[]>([])
const selectedTraceId = ref<string | null>(null)
const cleanupPreview = ref<ModelTraceCleanupPreview | null>(null)
const loading = ref(false)
const saving = ref(false)
const cleaning = ref(false)
const previewing = ref(false)
const errorMessage = ref('')
const outcomes: ModelTraceOutcome[] = ['succeeded', 'failed', 'blocked', 'client_cancelled', 'partial']
const filters = reactive<Pick<ModelTraceQueryParams, 'request_id' | 'user' | 'api_key' | 'requested_model' | 'outcome'>>({ request_id: '', user: '', api_key: '', requested_model: '', outcome: undefined })
const config = reactive<ModelTraceConfig>({ enabled: false, payload_capture_enabled: false, auto_cleanup_enabled: false, retention_days: 7 })
const listParams = computed<ModelTraceQueryParams>(() => ({ page: page.value, page_size: pageSize, request_id: filters.request_id || undefined, user: filters.user || undefined, api_key: filters.api_key || undefined, requested_model: filters.requested_model || undefined, outcome: filters.outcome || undefined }))
let listRequestVersion = 0
let configSaveRequestVersion = 0
let configReadVersion = 0

/** 同步加载索引与策略；详情正文不会随列表请求下载。 */
const load = async () => {
	const requestVersion = ++listRequestVersion
	const configVersionAtStart = configReadVersion
  loading.value = true
  errorMessage.value = ''
  try {
    const [listResult, configResult] = await Promise.all([modelTraceAPI.list(listParams.value), modelTraceAPI.getConfig()])
		if (requestVersion !== listRequestVersion) return
    items.value = listResult.items || []
    total.value = listResult.total || 0
		if (configVersionAtStart === configReadVersion) Object.assign(config, configResult)
  } catch {
		if (requestVersion === listRequestVersion) errorMessage.value = t('admin.modelTrace.errors.load')
  } finally {
		if (requestVersion === listRequestVersion) loading.value = false
  }
}

/** 以新筛选条件重置页码，避免空页掩盖实际调用记录。 */
const applyFilters = () => {
  page.value = 1
  void load()
}

/** 切换分页时保持当前索引筛选，不重新请求已选择详情。 */
const changePage = (nextPage: number) => {
  page.value = Math.max(1, nextPage)
  void load()
}

/** 打开独立会话回放窗口；列表请求始终不下载正文。 */
const openDetail = (traceID: string) => { selectedTraceId.value = traceID }

/** 保存完整设置快照，并由后端校验留存天数与启用策略。 */
const saveConfig = async () => {
	if (!Number.isInteger(config.retention_days) || config.retention_days < 1 || config.retention_days > 365) {
		errorMessage.value = t('admin.modelTrace.errors.retention')
		return
	}
	const requestVersion = ++configSaveRequestVersion
  saving.value = true
  errorMessage.value = ''
  try {
    const saved = await modelTraceAPI.updateConfig({ ...config, retention_days: Number(config.retention_days) })
		if (requestVersion === configSaveRequestVersion) {
			Object.assign(config, saved)
			configReadVersion++
		}
	} catch {
		if (requestVersion === configSaveRequestVersion) errorMessage.value = t('admin.modelTrace.errors.save')
	} finally {
		if (requestVersion === configSaveRequestVersion) saving.value = false
  }
}

/** 请求到期数据预览，供管理员看到影响范围后再确认删除。 */
const loadCleanupPreview = async () => {
  previewing.value = true
  errorMessage.value = ''
  try {
    cleanupPreview.value = await modelTraceAPI.previewCleanup()
  } catch {
    errorMessage.value = t('admin.modelTrace.errors.preview')
  } finally {
    previewing.value = false
  }
}

/** 仅在浏览器确认后执行手动清理，并刷新列表与预览状态。 */
const confirmCleanup = async () => {
  if (!cleanupPreview.value?.expired_traces || !window.confirm(t('admin.modelTrace.cleanup.confirm', { traces: cleanupPreview.value.expired_traces }))) return
  cleaning.value = true
  errorMessage.value = ''
  try {
    await modelTraceAPI.runCleanup()
    cleanupPreview.value = null
    selectedTraceId.value = null
    await load()
  } catch {
    errorMessage.value = t('admin.modelTrace.errors.cleanup')
  } finally {
    cleaning.value = false
  }
}

/** 将字节数格式化为管理员快速判断清理影响的可读单位。 */
const formatBytes = (bytes: number) => {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

/** 将服务端 ISO 时间转换为当前管理员浏览器本地时间。 */
const formatTime = (value: string) => new Date(value).toLocaleString()

onMounted(() => { void load() })
</script>

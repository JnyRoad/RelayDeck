<template>
  <BaseDialog :show="show" :title="t('admin.modelTrace.detail.title')" width="full" @close="close">
    <div class="min-h-[32rem] space-y-4" data-testid="model-trace-detail-dialog">
      <div class="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-slate-200 bg-slate-50 px-4 py-3 dark:border-dark-700 dark:bg-dark-800/70">
        <div class="min-w-0">
          <p class="text-xs font-medium uppercase tracking-[0.16em] text-slate-500 dark:text-slate-400">{{ t('admin.modelTrace.detail.trace') }}</p>
          <p class="truncate font-mono text-sm text-slate-800 dark:text-slate-100">{{ conversation?.current_trace_id || traceId || '—' }}</p>
        </div>
        <span v-if="conversation?.linked" class="rounded-full bg-emerald-100 px-2.5 py-1 text-xs font-medium text-emerald-800 dark:bg-emerald-900/40 dark:text-emerald-200">
          {{ t('admin.modelTrace.detail.linked', { source: conversation.link_source }) }}
        </span>
        <span v-else-if="conversation" data-testid="model-trace-unlinked" class="rounded-full bg-amber-100 px-2.5 py-1 text-xs font-medium text-amber-800 dark:bg-amber-900/40 dark:text-amber-200">
          {{ t('admin.modelTrace.detail.unlinked') }}
        </span>
      </div>

      <div class="flex gap-1 border-b border-slate-200 dark:border-dark-700" role="tablist" :aria-label="t('admin.modelTrace.detail.views')">
        <button data-testid="model-trace-view-chat" type="button" class="trace-view-tab" :class="activeView === 'chat' ? 'trace-view-tab-active' : ''" role="tab" :aria-selected="activeView === 'chat'" @click="activeView = 'chat'">
          {{ t('admin.modelTrace.detail.chat') }}
        </button>
        <button data-testid="model-trace-view-raw" type="button" class="trace-view-tab" :class="activeView === 'raw' ? 'trace-view-tab-active' : ''" role="tab" :aria-selected="activeView === 'raw'" @click="activeView = 'raw'">
          {{ t('admin.modelTrace.detail.rawChain') }}
        </button>
      </div>

      <div v-if="loading" class="flex min-h-80 items-center justify-center text-sm text-slate-500 dark:text-slate-400">{{ t('common.loading') }}</div>
      <p v-else-if="errorMessage" class="rounded-lg border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-200">{{ errorMessage }}</p>

      <section v-else-if="conversation && activeView === 'chat'" data-testid="model-trace-chat-scroll" class="trace-chat-scroll max-h-[calc(100vh-20rem)] min-h-[24rem] space-y-5 overflow-y-auto rounded-xl border border-slate-200 bg-[radial-gradient(circle_at_top,_rgba(20,184,166,0.09),transparent_28rem)] p-4 dark:border-dark-700 dark:bg-dark-900">
        <article v-for="turn in conversation.turns" :key="turn.trace.trace_id" :data-testid="`trace-chat-turn-${turn.trace.trace_id}`" :class="['trace-chat-turn space-y-3 rounded-xl border p-3', turn.trace.trace_id === conversation.current_trace_id ? 'trace-current-turn border-teal-400/70 bg-teal-50/70 ring-1 ring-teal-200 dark:border-teal-700 dark:bg-teal-950/20 dark:ring-teal-900' : 'border-transparent bg-white/70 dark:bg-dark-800/70']">
          <div class="flex items-center justify-between gap-3 text-xs text-slate-500 dark:text-slate-400">
            <span class="font-mono">{{ turn.trace.trace_id }}</span>
            <span v-if="turn.trace.trace_id === conversation.current_trace_id" class="rounded bg-teal-600 px-1.5 py-0.5 font-medium text-white">{{ t('admin.modelTrace.detail.currentTurn') }}</span>
          </div>
          <TraceBubble :label="t('admin.modelTrace.detail.user')" :trace-id="turn.trace.trace_id" :payload="findPayload(turn, 'client_request')" align="right" @copy="copyPayload(turn.trace.trace_id, $event)" />
          <TraceBubble :label="t('admin.modelTrace.detail.model')" :trace-id="turn.trace.trace_id" :payload="findReplyPayload(turn)" align="left" @copy="copyPayload(turn.trace.trace_id, $event)" />
        </article>
      </section>

      <section v-else-if="conversation" class="max-h-[calc(100vh-20rem)] min-h-[24rem] space-y-3 overflow-y-auto rounded-xl border border-slate-200 bg-slate-950 p-4 font-mono text-xs text-slate-100 dark:border-dark-700" data-testid="model-trace-raw-chain">
        <template v-if="currentTurn">
          <div class="rounded-lg border border-slate-700 bg-slate-900/80 p-3 text-slate-300">
            <p>{{ currentTurn.trace.route }} · {{ currentTurn.trace.requested_model || currentTurn.trace.response_model || '—' }}</p>
          </div>
          <TraceRawPayload v-for="payload in rootClientRequestPayloads" :key="payloadKey(currentTurn.trace.trace_id, payload)" :payload="payload" :content="payloadContent(currentTurn.trace.trace_id, payload)" @load="loadPayload(currentTurn.trace.trace_id, payload)" @copy="copyPayload(currentTurn.trace.trace_id, payload)" />
          <article v-for="attempt in currentTurn.attempts" :key="attempt.attempt_no" class="rounded-lg border border-slate-700 bg-slate-900/70 p-3">
            <div class="mb-2 flex flex-wrap items-center justify-between gap-2 text-slate-300">
              <span>{{ t('admin.modelTrace.detail.attempt', { number: attempt.attempt_no }) }} · {{ attempt.outcome }} · {{ attempt.status_code ?? '—' }}</span>
              <span>{{ attempt.account_snapshot || '—' }} · {{ attempt.upstream_model || '—' }}</span>
            </div>
            <p class="mb-3 break-all text-slate-500">{{ attempt.upstream_route || '—' }}</p>
            <TraceRawPayload v-for="payload in attemptPayloads(attempt.attempt_no)" :key="payloadKey(currentTurn.trace.trace_id, payload)" :payload="payload" :content="payloadContent(currentTurn.trace.trace_id, payload)" @load="loadPayload(currentTurn.trace.trace_id, payload)" @copy="copyPayload(currentTurn.trace.trace_id, payload)" />
          </article>
          <TraceRawPayload v-for="payload in rootClientResultPayloads" :key="payloadKey(currentTurn.trace.trace_id, payload)" :payload="payload" :content="payloadContent(currentTurn.trace.trace_id, payload)" @load="loadPayload(currentTurn.trace.trace_id, payload)" @copy="copyPayload(currentTurn.trace.trace_id, payload)" />
        </template>
      </section>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, ref, watch, type PropType } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { modelTraceAPI } from '@/api/admin/modelTrace'
import type { ModelTraceConversation, ModelTraceDetail, ModelTracePayload, ModelTracePayloadKind } from '@/api/admin/modelTrace'

const props = defineProps<{ show: boolean; traceId: string | null }>()
const emit = defineEmits<{ (event: 'close'): void }>()
const { t } = useI18n()

const conversation = ref<ModelTraceConversation | null>(null)
const loading = ref(false)
const errorMessage = ref('')
const activeView = ref<'chat' | 'raw'>('chat')
const contents = ref<Record<string, ModelTracePayload>>({})
let loadVersion = 0

/** Derive a stable browser-only lookup key for one selected trace payload. */
const payloadKey = (traceID: string, payload: ModelTracePayload) => `${traceID}:${payload.kind}:${payload.attempt_no}`

/** Return the selected root call from the replay result for its raw chain. */
const currentTurn = computed<ModelTraceDetail | null>(() => conversation.value?.turns.find((turn) => turn.trace.trace_id === conversation.value?.current_trace_id) || null)

/** Keep the client request before, and the visible result after, upstream attempts. */
const rootClientRequestPayloads = computed(() => currentTurn.value?.payloads.filter((payload) => payload.attempt_no === 0 && payload.kind === 'client_request') || [])
const rootClientResultPayloads = computed(() => currentTurn.value?.payloads.filter((payload) => payload.attempt_no === 0 && payload.kind !== 'client_request') || [])

/** Locate one payload metadata record by exact kind within a replay turn. */
const findPayload = (turn: ModelTraceDetail, kind: ModelTracePayloadKind) => turn.payloads.find((payload) => payload.kind === kind && payload.attempt_no === 0)

/** Prefer a client response, then a client-visible error, as the model bubble. */
const findReplyPayload = (turn: ModelTraceDetail) => findPayload(turn, 'client_response') || findPayload(turn, 'error_response')

/** Return payloads for exactly one ordered transport attempt. */
const attemptPayloads = (attemptNo: number) => currentTurn.value?.payloads.filter((payload) => payload.attempt_no === attemptNo) || []

/** Read a previously selected body without mutating metadata returned by list/detail APIs. */
const payloadContent = (traceID: string, payload: ModelTracePayload) => contents.value[payloadKey(traceID, payload)]?.content || ''

/** Load one body only after the dialog is open and its metadata marks it readable. */
const loadPayload = async (traceID: string, payload?: ModelTracePayload, expectedVersion = loadVersion) => {
  if (!payload || payload.content_status !== 'available') return
  const key = payloadKey(traceID, payload)
  if (contents.value[key]?.content !== undefined) return
  try {
    const loaded = await modelTraceAPI.getPayload(traceID, payload.kind, payload.attempt_no)
    if (expectedVersion !== loadVersion) return
    contents.value = { ...contents.value, [key]: loaded }
  } catch {
    if (expectedVersion === loadVersion) errorMessage.value = t('admin.modelTrace.errors.detail')
  }
}

/** Load the exact protocol-confirmed replay and then only client chat bodies. */
const loadConversation = async () => {
  if (!props.show || !props.traceId) return
  const version = ++loadVersion
  loading.value = true
  errorMessage.value = ''
  conversation.value = null
  contents.value = {}
  activeView.value = 'chat'
  try {
    const result = await modelTraceAPI.getConversation(props.traceId)
    if (version !== loadVersion) return
    conversation.value = result
    await Promise.all(result.turns.flatMap((turn) => [
      loadPayload(turn.trace.trace_id, findPayload(turn, 'client_request'), version),
      loadPayload(turn.trace.trace_id, findReplyPayload(turn), version),
    ]))
  } catch {
    if (version === loadVersion) errorMessage.value = t('admin.modelTrace.errors.detail')
  } finally {
    if (version === loadVersion) loading.value = false
  }
}

/** Record a content-free copy event before placing the selected body on the clipboard. */
const copyPayload = async (traceID: string, payload: ModelTracePayload) => {
  const version = loadVersion
  const content = payloadContent(traceID, payload)
  if (!content || !navigator.clipboard) return
  try {
    await modelTraceAPI.recordAccessEvent(traceID, payload.kind, payload.attempt_no)
    await navigator.clipboard.writeText(content)
  } catch {
    if (version === loadVersion) errorMessage.value = t('admin.modelTrace.errors.copy')
  }
}

/** Close clears the active dialog session so a later row click cannot render stale text. */
const close = () => {
  loadVersion++
  conversation.value = null
  contents.value = {}
  emit('close')
}

watch(() => [props.show, props.traceId] as const, ([show, traceID]) => {
  if (show && traceID) void loadConversation()
}, { immediate: true })

const TraceBubble = defineComponent({
  name: 'TraceBubble',
  props: {
    label: { type: String, required: true },
    traceId: { type: String, required: true },
    payload: { type: Object as PropType<ModelTracePayload | undefined>, default: undefined },
    align: { type: String as PropType<'left' | 'right'>, required: true },
  },
  emits: ['copy'],
  setup(componentProps, { emit }) {
    return () => h('section', { class: componentProps.align === 'right' ? 'ml-auto max-w-[92%]' : 'mr-auto max-w-[92%]' }, [
      h('p', { class: 'mb-1 px-1 text-xs font-medium text-slate-500 dark:text-slate-400' }, componentProps.label),
      h('div', { class: componentProps.align === 'right' ? 'rounded-2xl rounded-tr-sm bg-teal-600 px-4 py-3 text-sm text-white shadow-sm' : 'rounded-2xl rounded-tl-sm bg-slate-100 px-4 py-3 text-sm text-slate-800 shadow-sm dark:bg-dark-700 dark:text-slate-100' }, [
        componentProps.payload
          ? h('div', { class: 'space-y-2' }, [
            h('pre', { class: 'whitespace-pre-wrap break-words font-sans text-sm leading-6' }, payloadContent(componentProps.traceId, componentProps.payload)
              || (componentProps.payload.content_status === 'available'
                ? t('admin.modelTrace.detail.loadingBody')
                : t('admin.modelTrace.detail.contentUnavailable', { status: componentProps.payload.content_status }))),
            payloadContent(componentProps.traceId, componentProps.payload) ? h('button', { type: 'button', class: 'text-xs underline underline-offset-2 opacity-80 hover:opacity-100', onClick: () => emit('copy', componentProps.payload) }, t('common.copy')) : null,
          ])
          : h('p', { class: 'italic opacity-70' }, t('admin.modelTrace.detail.contentUnavailable', { status: 'not_captured' })),
      ]),
    ])
  },
})

const TraceRawPayload = defineComponent({
  name: 'TraceRawPayload',
  props: {
    payload: { type: Object as PropType<ModelTracePayload>, required: true },
    content: { type: String, default: '' },
  },
  emits: ['load', 'copy'],
  setup(componentProps, { emit }) {
    return () => h('section', { class: 'mb-3 rounded-md border border-slate-700 bg-slate-950/70 p-3' }, [
      h('div', { class: 'mb-2 flex flex-wrap items-center justify-between gap-2 text-slate-400' }, [
        h('span', `${componentProps.payload.kind} · ${componentProps.payload.content_type || 'text/plain'}`),
        componentProps.content
          ? h('button', { type: 'button', class: 'text-teal-300 underline underline-offset-2', onClick: () => emit('copy', componentProps.payload) }, t('common.copy'))
          : h('button', { type: 'button', class: 'text-teal-300 underline underline-offset-2 disabled:opacity-50', disabled: componentProps.payload.content_status !== 'available', onClick: () => emit('load', componentProps.payload) }, t('admin.modelTrace.detail.loadBody')),
      ]),
      h('p', { class: 'mb-2 break-all text-[11px] leading-4 text-slate-500', 'data-testid': 'model-trace-payload-metadata' }, t('admin.modelTrace.detail.payloadMetadata', {
        status: componentProps.payload.capture_status || 'not_captured',
        original: componentProps.payload.original_bytes ?? 0,
        stored: componentProps.payload.stored_bytes ?? 0,
        hash: componentProps.payload.sha256 || '—',
      })),
      componentProps.content
        ? h('pre', { class: 'max-h-80 overflow-auto whitespace-pre-wrap break-words text-slate-100' }, componentProps.content)
        : h('p', { class: 'text-slate-500' }, t('admin.modelTrace.detail.contentUnavailable', { status: componentProps.payload.content_status })),
    ])
  },
})
</script>

<style scoped>
.trace-view-tab { @apply -mb-px border-b-2 border-transparent px-3 py-2 text-sm font-medium text-slate-500 transition-colors hover:text-slate-800 dark:text-slate-400 dark:hover:text-slate-100; }
.trace-view-tab-active { @apply border-teal-500 text-teal-700 dark:text-teal-300; }
.trace-chat-scroll { scrollbar-color: rgba(13, 148, 136, 0.55) transparent; }
</style>

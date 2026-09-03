export default {
  modelTrace: {
    tab: 'Model call traces',
    settings: {
      title: 'Tracing and retention',
      hint: 'Off by default. Bodies are redacted, bounded, and encrypted; oversized bodies keep metadata only.',
      enabled: 'Enable call indexes',
      payload: 'Store request and response bodies',
      autoCleanup: 'Automatically clean expired records',
      retention: 'Retention days',
    },
    filters: { requestId: 'Request ID', model: 'Requested model', allOutcomes: 'All outcomes' },
    columns: { time: 'Time', route: 'Route', model: 'Model', result: 'Result', duration: 'Duration' },
    actions: { previewCleanup: 'Preview cleanup', cleanup: 'Clean expired records' },
    cleanup: { preview: 'This removes {traces} calls and {payloads} bodies (about {bytes}).', confirm: 'Clean {traces} expired model calls? This cannot be undone.' },
    pagination: { total: '{total} calls total' },
    empty: 'No model calls match these filters.',
    detail: { title: 'Model call detail', contentUnavailable: 'Body is unavailable ({status}). Unstored, truncated, and undecryptable content is never displayed.' },
    errors: { load: 'Unable to load model call traces.', detail: 'Unable to load call detail.', save: 'Unable to save the tracing policy.', preview: 'Unable to create a cleanup preview.', cleanup: 'Unable to clean expired calls.', copy: 'Unable to copy the body.' },
  },
}

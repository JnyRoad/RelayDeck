export default {
  modelTrace: {
    tab: 'Model call traces',
    settings: {
      title: 'Tracing and retention',
      hint: 'Off by default. Full text request and response bodies are encrypted; non-text and credential fields are excluded, and records expire by retention days.',
      enabled: 'Enable call indexes',
      payload: 'Store request and response bodies',
      autoCleanup: 'Automatically clean expired records',
      retention: 'Retention days',
    },
    filters: { requestId: 'Request ID', user: 'User', apiKey: 'API key', model: 'Requested model', allOutcomes: 'All outcomes' },
    columns: { time: 'Time', user: 'User', apiKey: 'API key', route: 'Route', model: 'Model', result: 'Result', duration: 'Duration' },
    actions: { previewCleanup: 'Preview cleanup', cleanup: 'Clean expired records' },
    cleanup: { preview: 'This removes {traces} calls, {attempts} upstream attempts, and {payloads} bodies (about {bytes}).', confirm: 'Clean {traces} expired model calls? This cannot be undone.' },
    pagination: { total: '{total} calls total' },
    empty: 'No model calls match these filters.',
    detail: {
      title: 'Complete call chain', trace: 'Current trace', linked: 'Linked by {source}', unlinked: 'No verifiable conversation link was found; only this call is shown.', views: 'Call detail views', chat: 'Conversation replay', rawChain: 'Raw chain', currentTurn: 'Current call', user: 'User input', model: 'Model output', attempt: 'Upstream attempt #{number}', loadBody: 'Load body', loadingBody: 'Loading body…', payloadMetadata: 'Status: {status} · Original: {original} B · Stored: {stored} B · SHA-256: {hash}', contentUnavailable: 'Body is unavailable ({status}). Unstored, unreadable, and undecryptable content is never displayed.',
    },
    errors: { load: 'Unable to load model call traces.', detail: 'Unable to load call detail.', save: 'Unable to save the tracing policy.', retention: 'Retention days must be an integer between 1 and 365.', preview: 'Unable to create a cleanup preview.', cleanup: 'Unable to clean expired calls.', copy: 'Unable to copy the body.' },
  },
}

export default {
  modelTrace: {
    tab: '模型调用追踪',
    settings: {
      title: '追踪与保留策略',
      hint: '默认关闭。可保存完整文本请求与响应并加密存储；非文本和凭据字段不会入库，到期记录按保留天数清理。',
      enabled: '启用调用索引',
      payload: '保存请求与响应正文',
      autoCleanup: '自动清理到期记录',
      retention: '保留天数',
    },
    filters: { requestId: '请求 ID', user: '使用用户', apiKey: '使用 Key', model: '请求模型', allOutcomes: '全部结果' },
    columns: { time: '时间', user: '用户', apiKey: 'API Key', route: '入口', model: '模型', result: '结果', duration: '耗时' },
    actions: { previewCleanup: '预览清理', cleanup: '清理到期记录' },
    cleanup: { preview: '将清理 {traces} 条调用、{attempts} 次上游尝试、{payloads} 份正文（约 {bytes}）。', confirm: '确认清理 {traces} 条到期模型调用？此操作不可恢复。' },
    pagination: { total: '共 {total} 条调用' },
    empty: '没有符合筛选条件的模型调用。',
    detail: {
      title: '完整调用链路', trace: '当前调用', linked: '已按 {source} 关联会话', unlinked: '未找到可验证的会话关联，仅展示此条调用。', views: '调用详情视图', chat: '会话回放', rawChain: '原始链路', currentTurn: '当前调用', user: '用户输入', model: '模型返回', attempt: '上游尝试 #{number}', loadBody: '加载正文', loadingBody: '正在加载正文…', payloadMetadata: '状态：{status} · 原始：{original} B · 保存：{stored} B · SHA-256：{hash}', contentUnavailable: '正文不可查看（{status}）。未保存、不可读取或无法解密的内容不会展示。',
    },
    errors: { load: '无法加载模型调用追踪。', detail: '无法加载调用详情。', save: '保存追踪策略失败。', retention: '保留天数必须是 1 到 365 的整数。', preview: '无法生成清理预览。', cleanup: '清理到期调用失败。', copy: '无法复制正文。' },
  },
}

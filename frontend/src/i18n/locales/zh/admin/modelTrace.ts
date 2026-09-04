export default {
  modelTrace: {
    tab: '模型调用追踪',
    settings: {
      title: '追踪与保留策略',
      hint: '默认关闭。正文在脱敏、限长并加密后保存；大于上限的正文仅保留元数据。',
      enabled: '启用调用索引',
      payload: '保存请求与响应正文',
      autoCleanup: '自动清理到期记录',
      retention: '保留天数',
    },
    filters: { requestId: '请求 ID', model: '请求模型', allOutcomes: '全部结果' },
    columns: { time: '时间', route: '入口', model: '模型', result: '结果', duration: '耗时' },
    actions: { previewCleanup: '预览清理', cleanup: '清理到期记录' },
    cleanup: { preview: '将清理 {traces} 条调用、{payloads} 份正文（约 {bytes}）。', confirm: '确认清理 {traces} 条到期模型调用？此操作不可恢复。' },
    pagination: { total: '共 {total} 条调用' },
    empty: '没有符合筛选条件的模型调用。',
    detail: { title: '模型调用详情', contentUnavailable: '正文不可查看（{status}）。未保存、超限截断或无法解密的内容不会展示。' },
    errors: { load: '无法加载模型调用追踪。', detail: '无法加载调用详情。', save: '保存追踪策略失败。', preview: '无法生成清理预览。', cleanup: '清理到期调用失败。', copy: '无法复制正文。' },
  },
}

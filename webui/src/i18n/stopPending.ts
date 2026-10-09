const en = {
  status: 'Stop pending', retry: 'Retry stop', review: 'Review pending stop',
  description: 'Stop has not completed. Runtime ownership is retained. Retry stop; do not start or submit another close operation.',
}
const locales = {
  'en-US': en,
  'zh-CN': { status: '停止待处理', retry: '重试停止', review: '查看待处理停止', description: '停止尚未完成，运行归属仍保留。请重试停止，不要启动或再次提交平仓操作。' },
  'zh-TW': { status: '停止待處理', retry: '重試停止', review: '查看待處理停止', description: '停止尚未完成，執行歸屬仍保留。請重試停止，不要啟動或再次提交平倉操作。' },
}
export function withStopPending(language: string, bundle: Record<string, unknown>) {
  return { ...bundle, stopPending: locales[language as keyof typeof locales] ?? en }
}

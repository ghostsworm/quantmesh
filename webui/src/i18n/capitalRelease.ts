const en = {
  successTitle: 'Capital released', successDetail: 'Released {{amount}} USDT. This resets verified stale accounting; it does not close positions.',
  partialTitle: 'Capital partially released', partialDetail: 'Released {{amount}} USDT; remaining owners require reconciliation. This was not an all-or-nothing operation.',
  blockedTitle: 'Capital release not completed', blockedDetail: 'No capital was released by this request. Verify positions, orders and accounting before retrying.',
  unknownTitle: 'Capital release outcome unverified', unknownDetail: 'The response was interrupted or invalid. Some capital may have been released; verify current accounting before retrying.',
  refreshTitle: 'Capital snapshot could not be refreshed', refreshDetail: 'Displayed allocation may be stale. Refresh and reconcile before another release.',
}
export const capitalReleaseLocales = {
  'en-US': en,
  'zh-CN': {
    successTitle: '资金已释放', successDetail: '已释放 {{amount}} USDT。本操作仅恢复已核实的陈旧记账，不执行平仓。',
    partialTitle: '资金部分释放', partialDetail: '已释放 {{amount}} USDT，其余归属仍需核账。本次操作不是全部成功或全部回滚。',
    blockedTitle: '资金释放未完成', blockedDetail: '本次请求未释放资金。请核实持仓、委托和资金账本后再重试。',
    unknownTitle: '资金释放结果未核实', unknownDetail: '响应中断或数据无效，可能已有部分资金释放；重试前请核实当前账本。',
    refreshTitle: '资金快照刷新失败', refreshDetail: '当前显示的分配数据可能已过期，请刷新并核账后再释放。',
  },
  'zh-TW': {
    successTitle: '資金已釋放', successDetail: '已釋放 {{amount}} USDT。本操作僅恢復已核實的陳舊記帳，不執行平倉。',
    partialTitle: '資金部分釋放', partialDetail: '已釋放 {{amount}} USDT，其餘歸屬仍需核帳。本次操作不是全部成功或全部回滾。',
    blockedTitle: '資金釋放未完成', blockedDetail: '本次請求未釋放資金。請核實持倉、委託和資金帳本後再重試。',
    unknownTitle: '資金釋放結果未核實', unknownDetail: '回應中斷或資料無效，可能已有部分資金釋放；重試前請核實目前帳本。',
    refreshTitle: '資金快照更新失敗', refreshDetail: '目前顯示的分配資料可能已過期，請更新並核帳後再釋放。',
  },
}
export function withCapitalRelease(language: string, bundle: Record<string, unknown>) {
  const resources = capitalReleaseLocales[language as keyof typeof capitalReleaseLocales] || en
  return { ...bundle, capitalRelease: resources }
}

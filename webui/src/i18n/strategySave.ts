const en = {
  refreshFailed: 'Configuration saved, but refreshing the view failed. Reload before making further changes.',
  applied: 'Configuration saved and runtime parameters applied.',
  failed: 'Configuration saved, but runtime application failed. Review runtime state before retrying.',
  notRunning: 'Configuration saved. This bot is not running; runtime application has not been validated.',
  unverified: 'Configuration saved, but runtime application is unverified. Review runtime state before retrying.',
  unknownSave: 'Save result is unverified. Reload configuration and check runtime state before retrying.',
}
const locales = {
  'en-US': en,
  'zh-CN': {
    refreshFailed: '配置已保存，但页面刷新失败。继续修改前请重新加载。',
    applied: '配置已保存，运行时参数已应用。',
    failed: '配置已保存，但运行时应用失败。重试前请检查运行状态。',
    notRunning: '配置已保存，此 Bot 未运行，尚未验证运行时应用。',
    unverified: '配置已保存，但运行时应用未核实。重试前请检查运行状态。',
    unknownSave: '保存结果未核实。重试前请重新加载配置并检查运行状态。',
  },
  'zh-TW': {
    refreshFailed: '配置已保存，但頁面重新整理失敗。繼續修改前請重新載入。',
    applied: '配置已保存，執行時參數已套用。',
    failed: '配置已保存，但執行時套用失敗。重試前請檢查執行狀態。',
    notRunning: '配置已保存，此 Bot 未執行，尚未驗證執行時套用。',
    unverified: '配置已保存，但執行時套用未核實。重試前請檢查執行狀態。',
    unknownSave: '保存結果未核實。重試前請重新載入配置並檢查執行狀態。',
  },
}
export function withStrategySave(language: string, bundle: Record<string, unknown>) {
  return { ...bundle, strategySave: locales[language as keyof typeof locales] ?? en }
}

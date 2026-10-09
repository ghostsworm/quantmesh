const en = {
  rejected: 'The configuration request was rejected. Reload configuration and resolve the conflict or authorization error before retrying.',
  preflightUnverified: 'Order cancellation or position close was incomplete or unverified. Configuration was not submitted by this operation; inspect orders and inventory before continuing.',
  applied: 'Configuration saved. Reported runtime trading parameters were applied; this is not validation of every global setting.',
  failed: 'Configuration saved, but runtime application failed. Check runtime state before retrying.',
  unverified: 'Configuration saved, but runtime application is unverified. Check runtime state before retrying.',
  restartRequired: 'Configuration saved. Some changes require restart and are not yet validated at runtime.',
  notRunning: 'Configuration saved. One or more bots are not running, or no runtime application was confirmed.',
  unknownSave: 'Save result is unverified. Reload configuration and check runtime state before retrying.',
  manualStartRequired: 'Automatic start was withheld because application of the target bot configuration was not verified. Check saved configuration, orders, inventory and recovery status before using the normal start action.',
}
const locales = {
  'en-US': en,
  'zh-CN': {
    rejected: '配置请求被拒绝。请重新加载配置并处理冲突或权限错误后再重试。',
    preflightUnverified: '撤单或平仓未完成或未核实，本次未继续提交配置。请先核对订单和持仓。',
    applied: '配置已保存，回执列出的运行时交易参数已应用；这不代表全部全局设置已验收。',
    failed: '配置已保存，但运行时应用失败。重试前请检查运行状态。',
    unverified: '配置已保存，但运行时应用未核实。重试前请检查运行状态。',
    restartRequired: '配置已保存，部分变更需要重启，尚未验证运行时生效。',
    notRunning: '配置已保存，部分 Bot 未运行，或没有确认任何运行时应用。',
    unknownSave: '保存结果未核实。重试前请重新加载配置并检查运行状态。',
    manualStartRequired: '目标 Bot 的配置应用未核实，本次没有自动启动。请核对已保存配置、订单、持仓与恢复状态，再使用常规启动入口。',
  },
  'zh-TW': {
    rejected: '配置請求被拒絕。請重新載入配置並處理衝突或權限錯誤後再重試。',
    preflightUnverified: '撤單或平倉未完成或未核實，本次未繼續提交配置。請先核對訂單和持倉。',
    applied: '配置已保存，回執列出的執行時交易參數已套用；這不代表全部全域設定已驗收。',
    failed: '配置已保存，但執行時套用失敗。重試前請檢查執行狀態。',
    unverified: '配置已保存，但執行時套用未核實。重試前請檢查執行狀態。',
    restartRequired: '配置已保存，部分變更需要重新啟動，尚未驗證執行時生效。',
    notRunning: '配置已保存，部分 Bot 未執行，或沒有確認任何執行時套用。',
    unknownSave: '保存結果未核實。重試前請重新載入配置並檢查執行狀態。',
    manualStartRequired: '目標 Bot 的配置套用未核實，本次沒有自動啟動。請核對已保存配置、訂單、持倉與恢復狀態，再使用一般啟動入口。',
  },
}
export function withConfigSave(language: string, bundle: Record<string, unknown>) {
  return { ...bundle, configSave: locales[language as keyof typeof locales] ?? en }
}

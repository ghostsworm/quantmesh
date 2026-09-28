const en = {
  title: 'Execution exposure (all shared strategies)',
  missing: 'Execution ledger unavailable. Filled inventory alone does not prove opening capacity.',
  not_initialized: 'Inventory and order recovery is incomplete. Opening is unavailable.',
  reconciliation_required: 'Order or inventory reconciliation is required. Opening is unavailable.',
  quote_unavailable: 'A valid fresh valuation quote is unavailable. Opening is unavailable.',
  unknown: 'Execution evidence is unverified. Opening is unavailable.',
  ready: 'Evidence is ready. Every order still requires the executor’s other checks.',
  exhausted: 'Quantity or notional capacity is exhausted, including pending orders.',
  layersFull: 'No new lot capacity. An existing lot may still have quantity capacity.',
  position: 'Ledger filled quantity',
  pending: 'Pending / uncertain opening quantity',
  projected: 'Filled + pending quantity',
  notional: 'Projected notional exposure',
  layers: 'Filled + pending lots',
  limit: 'Limit: {{value}}',
  unlimited: 'No configured limit',
  unavailable: 'Unverified',
  note: 'No netting of opposite legs. Pending closes do not free opening capacity. Price moves may increase existing notional exposure.',
}

export const executionExposureLocales = {
  'en-US': en,
  'zh-CN': {
    title: '执行额度（所有共享策略）', missing: '执行账本不可用，不能仅凭已成交库存认定还有开仓额度。',
    not_initialized: '持仓与订单恢复尚未完成，不能开仓。', reconciliation_required: '订单或库存需要重新核账，不能开仓。',
    quote_unavailable: '缺少有效、新鲜的估值报价，不能开仓。', unknown: '执行证据未核实，不能开仓。',
    ready: '数据已就绪，每笔订单仍须通过执行器的其他检查。', exhausted: '包含在途订单的数量或名义额度已用尽。',
    layersFull: '没有新批次额度；已有批次仍可能有数量额度。', position: '账本已成交数量', pending: '在途／未知开仓数量',
    projected: '已成交＋在途数量', notional: '预计名义敞口', layers: '已成交＋在途批次', limit: '上限：{{value}}',
    unlimited: '未配置上限', unavailable: '未核实', note: '多空不相抵，待成交平仓不释放开仓额度。行情上涨可能扩大已有名义敞口。',
  },
  'zh-TW': {
    title: '執行額度（所有共用策略）', missing: '執行帳本不可用，不能僅憑已成交庫存認定還有開倉額度。',
    not_initialized: '持倉與訂單恢復尚未完成，不能開倉。', reconciliation_required: '訂單或庫存需要重新核帳，不能開倉。',
    quote_unavailable: '缺少有效、新鮮的估值報價，不能開倉。', unknown: '執行證據未核實，不能開倉。',
    ready: '資料已就緒，每筆訂單仍須通過執行器的其他檢查。', exhausted: '包含在途訂單的數量或名義額度已用盡。',
    layersFull: '沒有新批次額度；既有批次仍可能有數量額度。', position: '帳本已成交數量', pending: '在途／未知開倉數量',
    projected: '已成交＋在途數量', notional: '預計名義曝險', layers: '已成交＋在途批次', limit: '上限：{{value}}',
    unlimited: '未設定上限', unavailable: '未核實', note: '多空不相抵，待成交平倉不釋放開倉額度。行情上漲可能擴大既有名義曝險。',
  },
}

export function withExecutionExposure(language: string, bundle: Record<string, unknown>) {
  const localized = executionExposureLocales[language as keyof typeof executionExposureLocales] ?? en
  return { ...bundle, executionExposure: localized }
}

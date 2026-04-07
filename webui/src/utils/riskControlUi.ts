/** 将用户输入的监控交易对字符串解析为标准化符号列表 */
export function parseMonitorSymbolsInput(raw: string): string[] {
  return raw
    .split(/[,，\s\n]+/)
    .map((s) => s.trim().toUpperCase())
    .filter(Boolean)
}

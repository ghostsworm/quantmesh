/**
 * 判断当前路径是否为某个 Bot 工作区根路径 `/bots/:botId`（不含子页如 /dashboard）。
 */
export function isBotWorkspaceRoot(pathname: string, botPrefix: string): boolean {
  return pathname === botPrefix || pathname === `${botPrefix}/`
}

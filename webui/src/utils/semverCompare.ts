/**
 * 比較兩個語義化版本字符串（支持 v 前綴、主版本號與可選的 -prerelease）。
 * @returns 負數若 a < b，0 若相等，正數若 a > b
 */
export function compareSemver(a: string, b: string): number {
  const pa = parseSemver(a)
  const pb = parseSemver(b)
  for (let i = 0; i < 3; i++) {
    if (pa.parts[i] !== pb.parts[i]) {
      return pa.parts[i] - pb.parts[i]
    }
  }
  if (pa.pre === null && pb.pre === null) return 0
  if (pa.pre === null) return 1
  if (pb.pre === null) return -1
  return pa.pre.localeCompare(pb.pre, undefined, { numeric: true, sensitivity: 'base' })
}

/** remote 是否比 current 更新（嚴格大於） */
export function isRemoteNewerThanCurrent(current: string, remote: string): boolean {
  return compareSemver(remote.trim(), current.trim()) > 0
}

function parseSemver(s: string): { parts: [number, number, number]; pre: string | null } {
  const raw = s.replace(/^v/i, '').trim()
  const dash = raw.indexOf('-')
  const core = dash >= 0 ? raw.slice(0, dash) : raw
  const pre = dash >= 0 ? raw.slice(dash + 1) : null
  const seg = core.split('.').map((p) => {
    const n = parseInt(p, 10)
    return Number.isFinite(n) ? n : 0
  })
  const parts: [number, number, number] = [
    seg[0] ?? 0,
    seg[1] ?? 0,
    seg[2] ?? 0,
  ]
  return { parts, pre }
}

import { isRemoteNewerThanCurrent } from '../utils/semverCompare'

const CACHE_PREFIX = 'qm_opensource_release_'
const CACHE_TTL_MS = 6 * 60 * 60 * 1000

export interface OpenSourceUpdateResult {
  hasUpdate: boolean
  currentVersion: string
  remoteTag: string | null
}

interface CacheEntry {
  t: number
  tag: string | null
}

function cacheKey(repo: string): string {
  return `${CACHE_PREFIX}${repo.replace(/[^a-zA-Z0-9._-]/g, '_')}`
}

function readCache(repo: string): string | null | undefined {
  try {
    const raw = sessionStorage.getItem(cacheKey(repo))
    if (!raw) return undefined
    const parsed = JSON.parse(raw) as CacheEntry
    if (!parsed || typeof parsed.t !== 'number') return undefined
    if (Date.now() - parsed.t > CACHE_TTL_MS) {
      sessionStorage.removeItem(cacheKey(repo))
      return undefined
    }
    return parsed.tag
  } catch {
    return undefined
  }
}

function writeCache(repo: string, tag: string | null): void {
  try {
    const entry: CacheEntry = { t: Date.now(), tag }
    sessionStorage.setItem(cacheKey(repo), JSON.stringify(entry))
  } catch {
    /* ignore quota */
  }
}

/** 從環境讀取 owner/repo，默認主庫 */
export function getGithubRepoFromEnv(): string {
  const raw = import.meta.env.VITE_GITHUB_REPO as string | undefined
  const trimmed = raw?.trim()
  if (trimmed && /^[a-zA-Z0-9_.-]+\/[a-zA-Z0-9_.-]+$/.test(trimmed)) {
    return trimmed
  }
  return 'ghostsworm/quantmesh'
}

export function githubRepoWebUrl(repo: string): string {
  return `https://github.com/${repo}`
}

/**
 * 查詢 GitHub releases/latest 的 tag_name（帶 session 緩存）。
 * 失敗時返回 null，不拋錯。
 */
export async function fetchLatestReleaseTag(repo: string): Promise<string | null> {
  const cached = readCache(repo)
  if (cached !== undefined) {
    return cached
  }

  const [owner, name] = repo.split('/')
  if (!owner || !name || repo.split('/').length !== 2) {
    writeCache(repo, null)
    return null
  }
  const url = `https://api.github.com/repos/${owner}/${name}/releases/latest`

  try {
    const res = await fetch(url, {
      headers: {
        Accept: 'application/vnd.github+json',
      },
    })
    if (!res.ok) {
      writeCache(repo, null)
      return null
    }
    const data = (await res.json()) as { tag_name?: string }
    const tag = typeof data.tag_name === 'string' ? data.tag_name : null
    writeCache(repo, tag)
    return tag
  } catch {
    writeCache(repo, null)
    return null
  }
}

export async function checkOpenSourceUpdate(currentVersion: string): Promise<OpenSourceUpdateResult> {
  const repo = getGithubRepoFromEnv()
  const remoteTag = await fetchLatestReleaseTag(repo)
  if (!remoteTag || !currentVersion) {
    return { hasUpdate: false, currentVersion, remoteTag }
  }
  const hasUpdate = isRemoteNewerThanCurrent(currentVersion, remoteTag)
  return { hasUpdate, currentVersion, remoteTag }
}

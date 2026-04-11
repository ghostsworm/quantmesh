import { useEffect, useState } from 'react'
import {
  checkOpenSourceUpdate,
  githubRepoWebUrl,
  getGithubRepoFromEnv,
} from '../services/opensourceUpdate'

export interface OpenSourceUpdateState {
  hasUpdate: boolean
  remoteTag: string | null
  repoUrl: string
  loading: boolean
}

export function useOpenSourceUpdate(): OpenSourceUpdateState {
  const [hasUpdate, setHasUpdate] = useState(false)
  const [remoteTag, setRemoteTag] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const repo = getGithubRepoFromEnv()
  const repoUrl = githubRepoWebUrl(repo)

  useEffect(() => {
    const disabled =
      import.meta.env.VITE_DISABLE_OPEN_SOURCE_UPDATE === '1' ||
      import.meta.env.VITE_DISABLE_OPEN_SOURCE_UPDATE === 'true'
    if (disabled) {
      setLoading(false)
      return
    }

    let cancelled = false
    ;(async () => {
      try {
        const verRes = await fetch('/api/version')
        if (!verRes.ok || cancelled) {
          setLoading(false)
          return
        }
        const data = (await verRes.json()) as { version?: string; backend_version?: string }
        const v = (data.version || data.backend_version || '').trim()
        if (!v || cancelled) {
          setLoading(false)
          return
        }
        const result = await checkOpenSourceUpdate(v)
        if (!cancelled) {
          setHasUpdate(result.hasUpdate)
          setRemoteTag(result.remoteTag)
        }
      } catch {
        if (!cancelled) {
          setHasUpdate(false)
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
        }
      }
    })()

    return () => {
      cancelled = true
    }
  }, [])

  return { hasUpdate, remoteTag, repoUrl, loading }
}

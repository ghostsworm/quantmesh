export type DownloadMediaResult = { ok: true } | { ok: false; error: string }

/**
 * 通过 fetch 拉取资源并触发浏览器下载。同源或已正确配置 CORS 时可成功；
 * 失败时由调用方决定是否回退为打开新标签页。
 */
export async function downloadMediaFromUrl(
  url: string,
  filename: string
): Promise<DownloadMediaResult> {
  try {
    const res = await fetch(url, { mode: 'cors' })
    if (!res.ok) {
      return { ok: false, error: `HTTP ${res.status}` }
    }
    const blob = await res.blob()
    const objectUrl = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = objectUrl
    a.download = filename || 'download'
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(objectUrl)
    return { ok: true }
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    return { ok: false, error: msg }
  }
}

export async function togglePictureInPicture(video: HTMLVideoElement | null): Promise<void> {
  if (!video) return
  if (document.pictureInPictureElement === video) {
    await document.exitPictureInPicture()
  } else {
    await video.requestPictureInPicture()
  }
}

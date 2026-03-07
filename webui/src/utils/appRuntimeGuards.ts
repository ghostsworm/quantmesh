const AUTH_FLOW_PATHS = new Set([
  '/login',
  '/setup',
  '/config-setup',
  '/wizard',
])

function normalizePathname(pathname: string): string {
  const withoutQuery = pathname.split('?')[0]?.split('#')[0] ?? ''
  if (!withoutQuery || withoutQuery === '/') {
    return '/'
  }

  return withoutQuery.endsWith('/') ? withoutQuery.slice(0, -1) : withoutQuery
}

export function isAuthFlowPath(pathname: string): boolean {
  return AUTH_FLOW_PATHS.has(normalizePathname(pathname))
}

export function getInitialBackendProbeDelayMs(pathname: string): number {
  return isAuthFlowPath(pathname) ? 3000 : 0
}

export async function disableServiceWorkersForAuthFlow(pathname: string): Promise<void> {
  if (
    !isAuthFlowPath(pathname)
    || typeof window === 'undefined'
    || !('serviceWorker' in navigator)
  ) {
    return
  }

  const registrations = await navigator.serviceWorker.getRegistrations()
  await Promise.all(registrations.map((registration) => registration.unregister()))
}

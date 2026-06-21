export interface SidebarSearchItem {
  label: string
  group: string
  to: string
  keywords?: string[]
}

const normalizeSidebarQuery = (value: string): string => value.trim().toLowerCase()

export const filterSidebarItems = <T extends SidebarSearchItem>(items: T[], query: string): T[] => {
  const normalized = normalizeSidebarQuery(query)
  if (!normalized) return []

  return items.filter((item) => {
    const haystack = [item.label, item.group, item.to, ...(item.keywords || [])]
      .join(' ')
      .toLowerCase()
    return haystack.includes(normalized)
  })
}

/**
 * 统一时间格式化工具
 * 支持根据配置的时区显示时间
 */

// 常用时区列表
export const COMMON_TIMEZONES = [
  { value: 'UTC', label: 'UTC (UTC+0:00)', offset: 0 },
  { value: 'Pacific/Honolulu', label: 'Hawaii (UTC-10:00)', offset: -10 },
  { value: 'America/Los_Angeles', label: 'Pacific Time (UTC-8:00/-7:00)', offset: -8 },
  { value: 'America/Denver', label: 'Mountain Time (UTC-7:00/-6:00)', offset: -7 },
  { value: 'America/Chicago', label: 'Central Time (UTC-6:00/-5:00)', offset: -6 },
  { value: 'America/New_York', label: 'Eastern Time (UTC-5:00/-4:00)', offset: -5 },
  { value: 'America/Sao_Paulo', label: 'São Paulo (UTC-3:00)', offset: -3 },
  { value: 'Europe/London', label: 'London (UTC+0:00/+1:00)', offset: 0 },
  { value: 'Europe/Paris', label: 'Paris (UTC+1:00/+2:00)', offset: 1 },
  { value: 'Europe/Berlin', label: 'Berlin (UTC+1:00/+2:00)', offset: 1 },
  { value: 'Europe/Moscow', label: 'Moscow (UTC+3:00)', offset: 3 },
  { value: 'Asia/Dubai', label: 'Dubai (UTC+4:00)', offset: 4 },
  { value: 'Asia/Karachi', label: 'Karachi (UTC+5:00)', offset: 5 },
  { value: 'Asia/Mumbai', label: 'Mumbai (UTC+5:30)', offset: 5.5 },
  { value: 'Asia/Dhaka', label: 'Dhaka (UTC+6:00)', offset: 6 },
  { value: 'Asia/Bangkok', label: 'Bangkok (UTC+7:00)', offset: 7 },
  { value: 'Asia/Shanghai', label: 'Beijing (UTC+8:00)', offset: 8 },
  { value: 'Asia/Tokyo', label: 'Tokyo (UTC+9:00)', offset: 9 },
  { value: 'Australia/Sydney', label: 'Sydney (UTC+10:00/+11:00)', offset: 10 },
  { value: 'Pacific/Auckland', label: 'Auckland (UTC+12:00/+13:00)', offset: 12 },
]

// 根据语言获取默认时区
export function getDefaultTimezoneByLanguage(language: string): string {
  // 中文用户默认使用北京时间 (UTC+8)
  if (language.startsWith('zh')) {
    return 'Asia/Shanghai'
  }
  // 英文和其他语言默认使用 UTC
  return 'UTC'
}

// 格式化时间 - 完整日期时间
export function formatDateTime(
  timeStr: string | Date | null | undefined,
  timezone: string,
  locale: string = 'zh-CN'
): string {
  if (!timeStr) return '-'
  try {
    const date = typeof timeStr === 'string' ? new Date(timeStr) : timeStr
    return date.toLocaleString(locale, {
      timeZone: timezone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false,
    })
  } catch {
    return typeof timeStr === 'string' ? timeStr : '-'
  }
}

// 格式化时间 - 仅时间
export function formatTime(
  timeStr: string | Date | null | undefined,
  timezone: string,
  locale: string = 'zh-CN'
): string {
  if (!timeStr) return '-'
  try {
    const date = typeof timeStr === 'string' ? new Date(timeStr) : timeStr
    return date.toLocaleTimeString(locale, {
      timeZone: timezone,
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false,
    })
  } catch {
    return typeof timeStr === 'string' ? timeStr : '-'
  }
}

// 格式化时间 - 仅日期
export function formatDate(
  timeStr: string | Date | null | undefined,
  timezone: string,
  locale: string = 'zh-CN'
): string {
  if (!timeStr) return '-'
  try {
    const date = typeof timeStr === 'string' ? new Date(timeStr) : timeStr
    return date.toLocaleDateString(locale, {
      timeZone: timezone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    })
  } catch {
    return typeof timeStr === 'string' ? timeStr : '-'
  }
}

// 格式化时间 - 简短格式（不含秒）
export function formatDateTimeShort(
  timeStr: string | Date | null | undefined,
  timezone: string,
  locale: string = 'zh-CN'
): string {
  if (!timeStr) return '-'
  try {
    const date = typeof timeStr === 'string' ? new Date(timeStr) : timeStr
    return date.toLocaleString(locale, {
      timeZone: timezone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    })
  } catch {
    return typeof timeStr === 'string' ? timeStr : '-'
  }
}

// 格式化时间 - 带时区信息
export function formatDateTimeWithTimezone(
  timeStr: string | Date | null | undefined,
  timezone: string,
  locale: string = 'zh-CN'
): string {
  if (!timeStr) return '-'
  try {
    const date = typeof timeStr === 'string' ? new Date(timeStr) : timeStr
    const formatted = date.toLocaleString(locale, {
      timeZone: timezone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hour12: false,
    })
    const tzOffset = date.toLocaleString(locale, { timeZoneName: 'short', timeZone: timezone })
    const tzName = tzOffset.split(' ').pop()
    return `${formatted} ${tzName}`
  } catch {
    return typeof timeStr === 'string' ? timeStr : '-'
  }
}

// 获取时区偏移显示（如 UTC+8）
export function getTimezoneOffset(timezone: string): string {
  try {
    const now = new Date()
    const offset = -now.getTimezoneOffset() / 60
    const sign = offset >= 0 ? '+' : ''
    const hours = Math.abs(Math.floor(offset))
    const minutes = Math.abs(offset % 1) * 60
    return `UTC${sign}${hours}:${minutes.toString().padStart(2, '0')}`
  } catch {
    return timezone
  }
}

const LOG_LEVEL_CLASS_MAP = {
  info: {
    statusBadge: 'running',
    activityLevel: 'info'
  },
  warn: {
    statusBadge: 'busy',
    activityLevel: 'warn'
  },
  error: {
    statusBadge: 'error',
    activityLevel: 'error'
  }
}

function normalizeLogLevel(level) {
  if (typeof level !== 'string') {
    return ''
  }

  return level.trim().toLowerCase()
}

export function getLogLevelClass(level, target) {
  const normalizedLevel = normalizeLogLevel(level)
  const classMapping = LOG_LEVEL_CLASS_MAP[normalizedLevel]

  if (!classMapping) {
    return target === 'statusBadge' ? 'stopped' : ''
  }

  return classMapping[target] || ''
}

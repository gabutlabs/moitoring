export function filterQueues(queues, filters = {}) {
  const status = filters.status ?? 'all'
  const vhost = filters.vhost ?? 'all'
  const name = (filters.name ?? '').trim().toLowerCase()

  return queues.filter((queue) => {
    const matchesStatus = status === 'all'
      || (status === 'critical' && queue.status === 'down')
      || (status === 'warning' && queue.status === 'warning')
      || (status === 'healthy' && queue.status === 'healthy')
      || (status === 'stale' && queue.is_stale)
      || (status === 'unknown' && queue.status === 'unknown')
    const matchesVHost = vhost === 'all'
      || (vhost === '__unknown__' && !queue.vhost)
      || queue.vhost === vhost
    const matchesName = !name || queue.name.toLowerCase().includes(name)

    return matchesStatus && matchesVHost && matchesName
  })
}

export function queueVHosts(queues) {
  return [...new Set(queues.map((queue) => queue.vhost).filter(Boolean))].sort((left, right) => left.localeCompare(right))
}

export function filterUnhealthyQueues(queues) {
  return queues.filter((queue) => queue.status !== 'healthy' || queue.is_stale)
}

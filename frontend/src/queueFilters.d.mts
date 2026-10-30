export interface QueueFilterInput {
  name: string
  vhost: string
  status: string
  is_stale: boolean
  node?: string
}

export interface QueueFilters {
  status?: 'all' | 'critical' | 'warning' | 'healthy' | 'stale' | 'unknown' | string
  vhost?: string
  name?: string
}

export function filterQueues<T extends QueueFilterInput>(queues: T[], filters?: QueueFilters): T[]
export function queueVHosts(queues: QueueFilterInput[]): string[]
export function filterUnhealthyQueues<T extends QueueFilterInput>(queues: T[]): T[]

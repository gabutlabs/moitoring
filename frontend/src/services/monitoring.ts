export interface QueueStatus {
  vhost: string
  name: string
  node: string
  state: string
  status: 'healthy' | 'warning' | 'down' | 'unknown' | string
  messages: number | null
  messages_ready: number | null
  messages_unacknowledged: number | null
  consumers: number | null
  publish_rate: number | null
  delivery_rate: number | null
  observed_at: string
  is_stale: boolean
}

export interface WorkerStatus {
  host: string
  unit: string
  load_state: string
  expected_state: string
  active_state: string
  sub_state: string
  result: string
  exec_main_code: string
  exec_main_status: string
  status_text: string
  observed_at: string
  status: 'healthy' | 'down' | string
  is_stale: boolean
}

export interface SourceStatus {
  name: string
  status: 'up' | 'down' | 'not_configured' | string
  last_success_at?: string
  checked_at?: string
  last_error?: string
  failed_emails?: number
  last_email_error?: string
}

export interface MonitoringSnapshot {
  source: SourceStatus
  worker_source: SourceStatus
  queues: QueueStatus[]
  workers: WorkerStatus[]
}

export async function getMonitoringSnapshot(signal?: AbortSignal): Promise<MonitoringSnapshot> {
  const response = await fetch('/api/monitoring', {
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
    signal,
  })

  if (!response.ok) {
    throw new Error('Could not load monitoring status.')
  }

  return response.json() as Promise<MonitoringSnapshot>
}

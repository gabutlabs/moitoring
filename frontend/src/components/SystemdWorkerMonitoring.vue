<script setup lang="ts">
import type { MonitoringSnapshot, WorkerStatus } from '../services/monitoring'

defineProps<{
  snapshot: MonitoringSnapshot | null
  loading: boolean
  error: string
}>()

function sourceLabel(status?: string): string {
  if (status === 'up') return 'Connected'
  if (status === 'down') return 'Unavailable'
  if (status === 'not_configured') return 'Not configured'
  return 'Waiting for first check'
}

function workerLabel(worker: WorkerStatus): string {
  if (worker.is_stale) return worker.status === 'down' ? 'Down · stale' : 'Stale'
  return worker.status === 'healthy' ? 'Healthy' : 'Unexpected state'
}

function workerClass(worker: WorkerStatus): string {
  if (worker.is_stale) return 'bg-neutral-100 text-neutral-800'
  if (worker.status === 'healthy') return 'bg-emerald-100 text-emerald-950'
  return 'bg-red-100 text-red-900'
}

function displayValue(value?: string): string {
  return value?.trim() || 'Unavailable'
}

function displayTime(value?: string): string {
  if (!value) return 'No successful poll yet'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? 'Unknown' : date.toLocaleString()
}
</script>

<template>
  <UCard>
    <template #header>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <h2 class="text-base font-semibold text-neutral-900">Systemd workers</h2>
        <p class="text-sm font-medium text-neutral-700" aria-live="polite">
          {{ sourceLabel(snapshot?.worker_source.status) }}
        </p>
      </div>
    </template>

    <p v-if="error" role="alert" class="mb-4 rounded-md bg-red-50 px-4 py-3 text-sm text-red-900">
      {{ error }}
    </p>
    <p v-else-if="snapshot?.worker_source.status === 'down'" role="status" class="mb-4 text-sm text-red-800">
      {{ snapshot.worker_source.last_error || 'Could not read systemd unit state.' }}
    </p>
    <p v-else-if="snapshot?.worker_source.status === 'not_configured'" class="mb-4 text-sm leading-6 text-neutral-700">
      Add service names under <code>[systemd]</code> <code>units</code> in <code>config.ini</code>, then run Moitoring on Linux.
    </p>
    <p v-else-if="loading && !snapshot" class="mb-4 text-sm text-neutral-700" role="status">
      Loading systemd worker status…
    </p>

    <div v-if="snapshot?.workers.length" class="divide-y divide-neutral-200">
      <article v-for="worker in snapshot.workers" :key="`${worker.host}/${worker.unit}`" class="py-4 first:pt-0 last:pb-0">
        <div class="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div class="min-w-0">
            <h3 class="break-all text-sm font-semibold text-neutral-950">{{ worker.unit }}</h3>
            <p class="mt-1 break-all text-sm text-neutral-700">{{ worker.host }}</p>
          </div>
          <span class="w-fit rounded px-2 py-1 text-xs font-semibold" :class="workerClass(worker)">
            {{ workerLabel(worker) }}
          </span>
        </div>

        <dl class="mt-3 grid min-w-0 gap-x-6 gap-y-3 sm:grid-cols-2 lg:grid-cols-3">
          <div>
            <dt class="text-xs font-medium text-neutral-600">Unit loaded</dt>
            <dd class="mt-1 break-words text-sm text-neutral-900">{{ displayValue(worker.load_state) }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-neutral-600">Observed state</dt>
            <dd class="mt-1 break-words text-sm text-neutral-900">
              {{ displayValue(worker.active_state) }} / {{ displayValue(worker.sub_state) }}
            </dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-neutral-600">Expected state</dt>
            <dd class="mt-1 break-words text-sm text-neutral-900">{{ displayValue(worker.expected_state) }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-neutral-600">Last result</dt>
            <dd class="mt-1 break-words text-sm text-neutral-900">
              {{ displayValue(worker.result) }}<span v-if="worker.exec_main_status"> · exit {{ worker.exec_main_status }}</span>
            </dd>
          </div>
          <div v-if="worker.status_text">
            <dt class="text-xs font-medium text-neutral-600">Systemd detail</dt>
            <dd class="mt-1 break-words text-sm text-neutral-900">{{ worker.status_text }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium text-neutral-600">Observed</dt>
            <dd class="mt-1 break-words text-sm text-neutral-900">{{ displayTime(worker.observed_at) }}</dd>
          </div>
        </dl>
      </article>
    </div>
    <p v-else-if="snapshot?.worker_source.status === 'up'" class="py-2 text-sm leading-6 text-neutral-700">
      No configured worker units have been observed yet.
    </p>
  </UCard>
</template>

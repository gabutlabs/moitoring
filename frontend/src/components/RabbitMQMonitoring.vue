<script setup lang="ts">
import { computed, ref } from 'vue'
import type { MonitoringSnapshot, QueueStatus } from '../services/monitoring'
import QueueNameSearch from './QueueNameSearch.vue'
import { filterQueues, filterUnhealthyQueues, queueVHosts } from '../queueFilters.mjs'

const props = defineProps<{
  snapshot: MonitoringSnapshot | null
  loading: boolean
  error: string
  unhealthyOnly: boolean
}>()

const statusFilter = ref('all')
const vhostFilter = ref('all')
const queueNameQuery = ref('')
const queues = computed(() => props.snapshot?.queues ?? [])
const vhosts = computed(() => queueVHosts(queues.value))
const hasUnknownVHost = computed(() => queues.value.some((queue) => !queue.vhost))
const visibleQueues = computed(() => {
  if (props.unhealthyOnly) return filterUnhealthyQueues(queues.value)
  return filterQueues(queues.value, {
    status: statusFilter.value,
    vhost: vhostFilter.value,
    name: queueNameQuery.value,
  })
})

function sourceLabel(status?: string): string {
  if (status === 'up') return 'Connected'
  if (status === 'down') return 'Unavailable'
  if (status === 'not_configured') return 'Not configured'
  return 'Waiting for first check'
}

function queueLabel(queue: QueueStatus): string {
  if (queue.status === 'down') return queue.is_stale ? 'Critical · stale' : 'Critical'
  if (queue.is_stale) return 'Stale'
  if (queue.status === 'warning') return 'Warning'
  if (queue.status === 'healthy') return 'Healthy'
  return 'Unknown'
}

function queueClass(queue: QueueStatus): string {
  if (queue.status === 'down') return 'bg-red-100 text-red-900'
  if (queue.is_stale) return 'bg-neutral-100 text-neutral-800'
  if (queue.status === 'warning') return 'bg-amber-100 text-amber-950'
  if (queue.status === 'healthy') return 'bg-emerald-100 text-emerald-950'
  return 'bg-neutral-100 text-neutral-800'
}

function displayNumber(value: number | null | undefined): string {
  return value === null || value === undefined ? 'Unavailable' : new Intl.NumberFormat().format(value)
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
        <h2 class="text-base font-semibold text-neutral-900">
          {{ unhealthyOnly ? 'RabbitMQ needs attention' : 'RabbitMQ' }}
        </h2>
        <p class="text-sm font-medium text-neutral-700" aria-live="polite">
          {{ sourceLabel(snapshot?.source.status) }}
        </p>
      </div>
    </template>

    <p v-if="error" role="alert" class="mb-4 rounded-md bg-red-50 px-4 py-3 text-sm text-red-900">
      {{ error }}
    </p>
    <p v-else-if="snapshot?.source.status === 'down'" role="status" class="mb-4 text-sm text-red-800">
      {{ snapshot.source.last_error || 'RabbitMQ could not be reached.' }}
    </p>
    <p v-else-if="snapshot?.source.status === 'not_configured'" class="mb-4 text-sm leading-6 text-neutral-600">
      Configure a RabbitMQ Management API source and run the worker to collect queue status.
    </p>
    <p v-else-if="loading && !snapshot" class="mb-4 text-sm text-neutral-600" role="status">
      Loading RabbitMQ status…
    </p>

    <p v-if="snapshot?.source.failed_emails" role="status" class="mb-4 rounded-md bg-amber-50 px-4 py-3 text-sm text-amber-950">
      {{ snapshot.source.failed_emails }} alert email{{ snapshot.source.failed_emails === 1 ? '' : 's' }} could not be delivered.
      <span v-if="snapshot.source.last_email_error">Latest error: {{ snapshot.source.last_email_error }}</span>
    </p>

    <div v-if="snapshot?.queues.length && !unhealthyOnly" class="flex flex-wrap items-end justify-between gap-3">
      <QueueNameSearch v-model="queueNameQuery" />

      <div class="flex flex-wrap items-end justify-end gap-3" role="group" aria-label="Filter RabbitMQ queues">
        <label class="grid gap-1 text-xs font-medium text-neutral-700">
          <span>Status</span>
          <select v-model="statusFilter" class="min-h-10 cursor-pointer rounded-md border border-neutral-500 bg-white px-3 text-sm text-neutral-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800">
            <option value="all">All statuses</option>
            <option value="critical">Critical</option>
            <option value="warning">Warning</option>
            <option value="healthy">Healthy</option>
            <option value="stale">Stale</option>
            <option value="unknown">Unknown</option>
          </select>
        </label>

        <label class="grid gap-1 text-xs font-medium text-neutral-700">
          <span>VHost</span>
          <select v-model="vhostFilter" class="min-h-10 min-w-48 cursor-pointer rounded-md border border-neutral-500 bg-white px-3 text-sm text-neutral-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800">
            <option value="all">All vhosts</option>
            <option v-for="vhost in vhosts" :key="vhost" :value="vhost">{{ vhost }}</option>
            <option v-if="hasUnknownVHost" value="__unknown__">Unknown vhost</option>
          </select>
        </label>

      </div>
    </div>
    <p v-if="snapshot?.queues.length && !unhealthyOnly" class="mb-4 mt-2 text-right text-xs text-neutral-500" aria-live="polite">
      {{ visibleQueues.length }} of {{ queues.length }} queues
    </p>

    <div v-if="visibleQueues.length" class="overflow-x-auto">
      <table class="w-full min-w-[54rem] border-collapse text-left text-sm">
        <caption class="sr-only">RabbitMQ queue status and latest observations</caption>
        <thead>
          <tr class="border-b border-neutral-200 text-xs font-medium text-neutral-600">
            <th scope="col" class="px-3 py-3">Queue</th>
            <th scope="col" class="px-3 py-3">Node</th>
            <th scope="col" class="px-3 py-3">Status</th>
            <th scope="col" class="px-3 py-3">Messages</th>
            <th scope="col" class="px-3 py-3">Ready</th>
            <th scope="col" class="px-3 py-3">Unacknowledged</th>
            <th scope="col" class="px-3 py-3">Consumers</th>
            <th scope="col" class="px-3 py-3">Observed</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="queue in visibleQueues" :key="`${queue.vhost}/${queue.name}`" class="border-b border-neutral-100 last:border-0">
            <th scope="row" class="px-3 py-3 font-medium text-neutral-900">
              <span class="block">{{ queue.name }}</span>
              <span class="mt-1 block text-xs font-normal text-neutral-500">vhost {{ queue.vhost }}</span>
            </th>
            <td class="px-3 py-3 text-neutral-700">{{ queue.node || 'Unavailable' }}</td>
            <td class="px-3 py-3">
              <span class="inline-flex rounded px-2 py-1 text-xs font-semibold" :class="queueClass(queue)">
                {{ queueLabel(queue) }}
              </span>
              <span class="mt-1 block text-xs text-neutral-600">{{ queue.state || 'State unavailable' }}</span>
            </td>
            <td class="px-3 py-3 tabular-nums">{{ displayNumber(queue.messages) }}</td>
            <td class="px-3 py-3 tabular-nums">{{ displayNumber(queue.messages_ready) }}</td>
            <td class="px-3 py-3 tabular-nums">{{ displayNumber(queue.messages_unacknowledged) }}</td>
            <td class="px-3 py-3 tabular-nums">{{ displayNumber(queue.consumers) }}</td>
            <td class="px-3 py-3 text-neutral-600">{{ displayTime(queue.observed_at) }}</td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-else-if="snapshot?.source.status === 'up' && snapshot.queues.length && unhealthyOnly" class="py-5 text-sm leading-6 text-neutral-600">
      No queues need attention. All observed queues are healthy.
    </p>
    <p v-else-if="snapshot?.source.status === 'up' && snapshot.queues.length" class="py-5 text-sm leading-6 text-neutral-600">
      No queues match these filters.
    </p>
    <p v-else-if="snapshot?.source.status === 'up'" class="py-5 text-sm leading-6 text-neutral-600">
      No queues have been observed in the configured virtual hosts yet.
    </p>

    <p v-if="snapshot?.source.last_success_at" class="mt-4 text-xs text-neutral-500">
      Last successful poll: {{ displayTime(snapshot.source.last_success_at) }}
    </p>
  </UCard>
</template>

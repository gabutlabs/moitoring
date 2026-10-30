<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import ApiConnectionCard from '../components/ApiConnectionCard.vue'
import AccountControls from '../components/AccountControls.vue'
import DashboardSidebar from '../components/DashboardSidebar.vue'
import RabbitMQMonitoring from '../components/RabbitMQMonitoring.vue'
import SourceEmptyState from '../components/SourceEmptyState.vue'
import SystemdWorkerMonitoring from '../components/SystemdWorkerMonitoring.vue'
import { dashboardViewByKey } from '../dashboardViews.mjs'
import { getMonitoringSnapshot, type MonitoringSnapshot } from '../services/monitoring'

defineProps<{
  adminEmail: string
  signingOut: boolean
}>()

const emit = defineEmits<{ logout: [] }>()
const activeViewKey = ref('overview')
const activeView = computed(() => dashboardViewByKey(activeViewKey.value))
const snapshot = ref<MonitoringSnapshot | null>(null)
const loading = ref(true)
const monitoringError = ref('')
let refreshTimer: ReturnType<typeof setInterval> | undefined

async function refreshMonitoring(signal?: AbortSignal): Promise<void> {
  try {
    snapshot.value = await getMonitoringSnapshot(signal)
    monitoringError.value = ''
  } catch {
    if (!signal?.aborted) monitoringError.value = 'Could not load monitoring status.'
  } finally {
    if (!signal?.aborted) loading.value = false
  }
}

onMounted(() => {
  const controller = new AbortController()
  void refreshMonitoring(controller.signal)
  refreshTimer = setInterval(() => void refreshMonitoring(), 30_000)
})

onUnmounted(() => {
  if (refreshTimer) clearInterval(refreshTimer)
})
</script>

<template>
  <div class="min-h-screen bg-[#f3f5f2] text-[#1c2923] lg:grid lg:grid-cols-[15rem_minmax(0,1fr)]">
    <DashboardSidebar :active-view="activeViewKey" @select="activeViewKey = $event" />

    <main class="min-w-0">
      <header class="flex flex-col gap-5 border-b border-neutral-200 bg-white px-5 py-6 sm:px-8 sm:py-7 md:flex-row md:items-center md:justify-between">
        <div class="min-w-0">
          <h1 class="text-2xl font-semibold tracking-tight text-neutral-950 sm:text-3xl">
            {{ activeView.title }}
          </h1>
          <p class="mt-2 text-sm leading-6 text-neutral-600 sm:text-base">
            {{ activeView.description }}
          </p>
        </div>
        <AccountControls :admin-email="adminEmail" :signing-out="signingOut" @logout="emit('logout')" />
      </header>

      <section class="grid min-w-0 gap-5 px-5 py-6 sm:px-8 sm:py-8" aria-label="Current system status">
        <ApiConnectionCard />
        <SystemdWorkerMonitoring
          v-if="activeViewKey === 'overview' || activeViewKey === 'services'"
          :snapshot="snapshot"
          :loading="loading"
          :error="activeViewKey === 'services' ? monitoringError : ''"
        />
        <RabbitMQMonitoring
          v-if="activeViewKey === 'overview' || activeViewKey === 'queues'"
          :snapshot="snapshot"
          :loading="loading"
          :error="monitoringError"
          :unhealthy-only="activeViewKey === 'overview'"
        />
        <SourceEmptyState
          v-if="activeViewKey !== 'overview' && activeViewKey !== 'queues' && activeViewKey !== 'services'"
          :eyebrow="activeView.eyebrow"
          :title="activeView.emptyTitle"
          :description="activeView.emptyDescription"
          :details="activeView.emptyDetails"
        />
      </section>
    </main>
  </div>
</template>

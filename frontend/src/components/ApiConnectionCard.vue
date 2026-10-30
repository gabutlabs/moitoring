<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

type ConnectionState = 'checking' | 'connected' | 'offline'

const state = ref<ConnectionState>('checking')
const label = computed(() => ({
  checking: 'Checking connection',
  connected: 'Connected',
  offline: 'API unavailable',
})[state.value])
const detail = computed(() => ({
  checking: 'Waiting for the local API to respond.',
  connected: 'The local API returned a healthy status.',
  offline: 'Start the API with go run ./cmd/moitoring serve -p 8080.',
})[state.value])

onMounted(async () => {
  try {
    const response = await fetch('/api/health', { headers: { Accept: 'application/json' } })
    if (!response.ok) throw new Error('Health request failed')

    const payload: unknown = await response.json()
    if (typeof payload !== 'object' || payload === null || !('status' in payload) || payload.status !== 'ok') {
      throw new Error('Health response was not successful')
    }

    state.value = 'connected'
  } catch {
    state.value = 'offline'
  }
})
</script>

<template>
  <UCard class="h-full">
    <template #header>
      <h2 class="text-base font-semibold text-neutral-900">Application API</h2>
    </template>

    <div class="flex items-start gap-3">
      <span
        class="mt-1.5 size-2.5 shrink-0 rounded-full"
        :class="{
          'bg-neutral-500': state === 'checking',
          'bg-emerald-700': state === 'connected',
          'bg-red-700': state === 'offline',
        }"
        aria-hidden="true"
      />
      <div>
        <p class="font-medium text-neutral-900" aria-live="polite">{{ label }}</p>
        <p class="mt-1 text-sm leading-6 text-neutral-600">{{ detail }}</p>
      </div>
    </div>
  </UCard>
</template>

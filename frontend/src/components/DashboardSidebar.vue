<script setup lang="ts">
import { dashboardViews } from '../dashboardViews.mjs'

defineProps<{
  activeView: string
}>()

const emit = defineEmits<{
  select: [viewKey: string]
}>()
</script>

<template>
  <aside class="border-b border-neutral-200 bg-white px-4 py-4 sm:px-6 lg:min-h-screen lg:border-b-0 lg:border-r lg:px-5 lg:py-7">
    <div class="flex items-baseline justify-between gap-3 lg:block">
      <p class="text-lg font-semibold tracking-tight text-neutral-950">Moitoring</p>
      <p class="text-xs text-neutral-500 lg:mt-1">Operations monitor</p>
    </div>

    <div
      class="mt-4 grid grid-cols-3 gap-1 sm:gap-2 lg:mt-10 lg:flex lg:flex-col lg:gap-1"
      role="group"
      aria-label="Monitoring views"
    >
      <button
        v-for="view in dashboardViews"
        :key="view.key"
        type="button"
        class="min-h-12 rounded-md px-2 py-2 text-center text-xs font-medium leading-4 transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800 sm:text-sm lg:min-h-11 lg:px-3 lg:text-left"
        :class="activeView === view.key ? 'bg-emerald-100 text-emerald-950' : 'text-neutral-700 hover:bg-neutral-100'"
        :aria-pressed="activeView === view.key"
        @click="emit('select', view.key)"
      >
        {{ view.label }}
      </button>
    </div>

    <p class="mt-4 hidden border-t border-neutral-200 pt-4 text-xs text-neutral-500 lg:block">
      Read-only monitoring
    </p>
  </aside>
</template>

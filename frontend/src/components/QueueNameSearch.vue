<script setup lang="ts">
const props = defineProps<{
  modelValue: string
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

function updateQuery(event: Event): void {
  emit('update:modelValue', (event.target as HTMLInputElement).value)
}
</script>

<template>
  <form role="search" aria-label="Search queues" class="grid min-w-56 max-w-md flex-1 gap-1" @submit.prevent>
    <label for="queue-name-search" class="text-xs font-medium text-neutral-700">
      Queue name
    </label>
    <div class="flex gap-2">
      <input
        id="queue-name-search"
        type="search"
        name="queue-name"
        autocomplete="off"
        :value="props.modelValue"
        placeholder="Search queue names"
        class="min-h-10 min-w-0 flex-1 rounded-md border border-neutral-500 bg-white px-3 text-sm text-neutral-900 placeholder:text-neutral-600 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800"
        @input="updateQuery"
      >
      <button
        v-if="props.modelValue"
        type="button"
        class="min-h-10 cursor-pointer rounded-md px-2 text-xs font-medium text-neutral-700 hover:bg-neutral-100 active:bg-neutral-200 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800"
        aria-label="Clear queue name search"
        @click="emit('update:modelValue', '')"
      >
        Clear
      </button>
    </div>
  </form>
</template>

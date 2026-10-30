<script setup lang="ts">
defineProps<{
  adminEmail: string
  signingOut: boolean
}>()

const emit = defineEmits<{ logout: [] }>()
</script>

<template>
  <div class="flex items-center gap-4">
    <span class="hidden text-sm text-neutral-600 sm:inline">{{ adminEmail }}</span>
    <button
      type="button"
      class="min-h-11 rounded-md border border-neutral-500 px-4 py-2 text-sm font-medium text-neutral-800 hover:bg-neutral-100 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800 disabled:cursor-wait disabled:opacity-70"
      :disabled="signingOut"
      @click="emit('logout')"
    >
      {{ signingOut ? 'Signing out…' : 'Sign out' }}
    </button>
  </div>
</template>

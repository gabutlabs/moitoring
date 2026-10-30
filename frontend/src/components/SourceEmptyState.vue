<script setup lang="ts">
defineProps<{
  eyebrow: string
  title: string
  description: string
  details: string
}>()
</script>

<template>
  <UCard class="min-h-[17rem]">
    <section class="max-w-2xl py-2" aria-labelledby="source-empty-title">
      <p class="text-sm font-medium text-emerald-800">{{ eyebrow }}</p>
      <h2 id="source-empty-title" class="mt-2 text-xl font-semibold tracking-tight text-neutral-950">
        {{ title }}
      </h2>
      <p class="mt-3 leading-7 text-neutral-600">
        {{ description }}
      </p>
      <p class="mt-2 leading-7 text-neutral-600">
        {{ details }}
      </p>
    </section>
  </UCard>
</template>

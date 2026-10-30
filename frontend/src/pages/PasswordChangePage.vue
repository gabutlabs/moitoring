<script setup lang="ts">
import { ref } from 'vue'
import { changePassword } from '../services/auth'

const emit = defineEmits<{ changed: [] }>()

const password = ref('')
const confirmPassword = ref('')
const pending = ref(false)
const errorMessage = ref('')

async function submit() {
  errorMessage.value = ''
  pending.value = true
  try {
    await changePassword(password.value, confirmPassword.value)
    password.value = ''
    confirmPassword.value = ''
    emit('changed')
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : 'Could not update the password.'
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <main class="grid min-h-screen place-items-center px-5 py-10 sm:px-8">
    <section class="w-full max-w-md rounded-lg border border-neutral-200 bg-white p-6 sm:p-8" aria-labelledby="password-change-title">
      <p class="text-sm font-semibold text-emerald-800">Moitoring · administrator setup</p>
      <h1 id="password-change-title" class="mt-4 text-2xl font-semibold tracking-tight text-neutral-950">Set a new password</h1>
      <p class="mt-3 text-sm leading-6 text-neutral-600">
        Choose a password with at least 12 characters. This step is required before monitoring data can be opened.
      </p>

      <form class="mt-7 space-y-5" @submit.prevent="submit">
        <div>
          <label for="new-password" class="mb-2 block text-sm font-medium text-neutral-800">New password</label>
          <input
            id="new-password"
            v-model="password"
            type="password"
            autocomplete="new-password"
            required
            minlength="12"
            maxlength="72"
            class="min-h-11 w-full rounded-md border border-neutral-500 bg-white px-3 text-base text-neutral-950 focus-visible:border-emerald-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800"
            :disabled="pending"
            aria-describedby="password-guidance"
          />
          <p id="password-guidance" class="mt-2 text-xs leading-5 text-neutral-500">At least 12 characters and no more than 72 bytes.</p>
        </div>

        <div>
          <label for="confirm-password" class="mb-2 block text-sm font-medium text-neutral-800">Confirm new password</label>
          <input
            id="confirm-password"
            v-model="confirmPassword"
            type="password"
            autocomplete="new-password"
            required
            minlength="12"
            maxlength="72"
            class="min-h-11 w-full rounded-md border border-neutral-500 bg-white px-3 text-base text-neutral-950 focus-visible:border-emerald-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800"
            :disabled="pending"
          />
        </div>

        <p v-if="errorMessage" class="text-sm text-red-800" role="alert">{{ errorMessage }}</p>

        <button
          type="submit"
          class="min-h-11 w-full rounded-md bg-emerald-800 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800 disabled:cursor-wait disabled:opacity-70"
          :disabled="pending"
        >
          {{ pending ? 'Saving…' : 'Save password' }}
        </button>
      </form>
    </section>
  </main>
</template>

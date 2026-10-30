<script setup lang="ts">
import { ref } from 'vue'
import { signIn } from '../services/auth'

defineProps<{ adminEmail: string }>()
const emit = defineEmits<{ signedIn: [] }>()

const password = ref('')
const pending = ref(false)
const errorMessage = ref('')

async function submit() {
  errorMessage.value = ''
  pending.value = true
  try {
    await signIn(password.value)
    password.value = ''
    emit('signedIn')
  } catch (error) {
    errorMessage.value = error instanceof Error ? error.message : 'Could not sign in.'
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <main class="min-h-screen px-5 py-10 sm:px-8 sm:py-16">
    <div class="mx-auto grid min-h-[calc(100vh-5rem)] max-w-5xl items-center gap-12 lg:grid-cols-[1fr_24rem]">
      <section class="max-w-xl">
        <p class="text-sm font-semibold text-emerald-800">Moitoring</p>
        <h1 class="mt-5 text-4xl font-semibold tracking-tight text-neutral-950 sm:text-5xl">Monitoring status, in one place.</h1>
        <p class="mt-5 max-w-lg text-base leading-7 text-neutral-600">
          Review systemd worker states and RabbitMQ queue health from a single administrator account.
        </p>
      </section>

      <section class="rounded-lg border border-neutral-200 bg-white p-6 sm:p-8" aria-labelledby="login-title">
        <h2 id="login-title" class="text-xl font-semibold tracking-tight text-neutral-950">Administrator access</h2>
        <p class="mt-2 text-sm leading-6 text-neutral-600">{{ adminEmail || 'Administrator account' }}</p>

        <form class="mt-7 space-y-5" @submit.prevent="submit">
          <div>
            <label for="admin-password" class="mb-2 block text-sm font-medium text-neutral-800">Password</label>
            <input
              id="admin-password"
              v-model="password"
              type="password"
              autocomplete="current-password"
              required
              autofocus
              class="min-h-11 w-full rounded-md border border-neutral-500 bg-white px-3 text-base text-neutral-950 focus-visible:border-emerald-800 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800"
              :disabled="pending"
              aria-describedby="login-error"
            />
          </div>

          <p id="login-error" class="text-sm text-red-900" role="alert" aria-live="polite">{{ errorMessage }}</p>

          <button
            type="submit"
            class="min-h-11 w-full rounded-md bg-emerald-800 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800 disabled:cursor-wait disabled:opacity-70"
            :disabled="pending"
          >
            {{ pending ? 'Checking…' : 'Continue' }}
          </button>
        </form>
      </section>
    </div>
  </main>
</template>

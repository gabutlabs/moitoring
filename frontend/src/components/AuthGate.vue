<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterView } from 'vue-router'
import LoginPage from '../pages/LoginPage.vue'
import PasswordChangePage from '../pages/PasswordChangePage.vue'
import { getAuthSession, signOut as revokeSession, type AuthSession } from '../services/auth'

type Phase = 'checking' | 'unavailable' | 'signed-out' | 'signed-in'

const phase = ref<Phase>('checking')
const session = ref<AuthSession>({ authenticated: false, admin_email: '', must_change_password: false })
const message = ref('')
const signingOut = ref(false)

async function refreshSession() {
  phase.value = 'checking'
  message.value = ''
  try {
    session.value = await getAuthSession()
    phase.value = session.value.authenticated ? 'signed-in' : 'signed-out'
  } catch {
    phase.value = 'unavailable'
    message.value = 'Could not check administrator access. Confirm the local API is running, then try again.'
  }
}

async function signOut() {
  signingOut.value = true
  message.value = ''
  try {
    await revokeSession()
    session.value.authenticated = false
    session.value.must_change_password = false
    phase.value = 'signed-out'
  } catch (error) {
    message.value = error instanceof Error ? error.message : 'Could not sign out.'
  } finally {
    signingOut.value = false
  }
}

onMounted(refreshSession)
</script>

<template>
  <main v-if="phase === 'checking'" class="grid min-h-screen place-items-center px-5" aria-live="polite">
    <p class="text-sm text-neutral-600">Checking administrator session…</p>
  </main>

  <main v-else-if="phase === 'unavailable'" class="grid min-h-screen place-items-center px-5">
    <section class="w-full max-w-md rounded-lg border border-neutral-200 bg-white p-7" aria-labelledby="api-error-title">
      <h1 id="api-error-title" class="text-xl font-semibold text-neutral-950">Monitoring is unavailable</h1>
      <p class="mt-2 text-sm leading-6 text-neutral-600">{{ message }}</p>
      <button
        type="button"
        class="mt-6 min-h-11 rounded-md bg-emerald-800 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-900 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-emerald-800"
        @click="refreshSession"
      >
        Check again
      </button>
    </section>
  </main>

  <LoginPage
    v-else-if="phase === 'signed-out'"
    :admin-email="session.admin_email"
    @signed-in="refreshSession"
  />

  <PasswordChangePage
    v-else-if="session.must_change_password"
    @changed="refreshSession"
  />

  <div v-else-if="message">
    <div class="px-5 pt-4 sm:px-8" role="alert">
      <p class="mx-auto max-w-6xl rounded-md border border-red-300 bg-red-50 px-4 py-3 text-sm text-red-900">{{ message }}</p>
    </div>
    <RouterView v-slot="{ Component }">
      <component :is="Component" :admin-email="session.admin_email" :signing-out="signingOut" @logout="signOut" />
    </RouterView>
  </div>

  <RouterView v-else v-slot="{ Component }">
    <component :is="Component" :admin-email="session.admin_email" :signing-out="signingOut" @logout="signOut" />
  </RouterView>
</template>

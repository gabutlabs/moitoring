export interface AuthSession {
  authenticated: boolean
  admin_email: string
  must_change_password: boolean
}

async function request(path: string, options: RequestInit = {}): Promise<Response> {
  const response = await fetch(path, {
    ...options,
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      ...(options.body ? { 'Content-Type': 'application/json' } : {}),
      ...options.headers,
    },
  })

  if (!response.ok) {
    const payload: unknown = await response.json().catch(() => null)
    const message = typeof payload === 'object' && payload !== null && 'error' in payload && typeof payload.error === 'string'
      ? payload.error
      : 'The request could not be completed.'
    throw new Error(message)
  }

  return response
}

export async function getAuthSession(): Promise<AuthSession> {
  const response = await request('/api/auth/session')
  return response.json() as Promise<AuthSession>
}

export async function signIn(password: string): Promise<void> {
  await request('/api/auth/login', { method: 'POST', body: JSON.stringify({ password }) })
}

export async function changePassword(password: string, confirmPassword: string): Promise<void> {
  await request('/api/auth/password', {
    method: 'POST',
    body: JSON.stringify({ password, confirm_password: confirmPassword }),
  })
}

export async function signOut(): Promise<void> {
  await request('/api/auth/logout', { method: 'POST' })
}

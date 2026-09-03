import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post }
}))

import {
  cancelCodexAppServerLogin,
  createCodexAppServerAccount,
  getCodexAppServerLogin,
  startCodexAppServerLogin
} from '@/api/admin/accounts'

describe('admin official app-server login API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('uses the isolated managed-login endpoints and never sends a token payload', async () => {
    const login = {
      session_id: 'profile-1',
      login_id: 'official-login-1',
      mode: 'device_code',
      status: 'pending',
      verification_url: 'https://auth.openai.com/codex/device',
      user_code: 'ABCD-1234'
    }
    post.mockResolvedValueOnce({ data: login })
    get.mockResolvedValueOnce({ data: { ...login, status: 'completed' } })
    post.mockResolvedValueOnce({ data: { id: 9, name: '个人官方运行时' } })
    post.mockResolvedValueOnce({ data: { status: 'cancelled' } })

    await expect(startCodexAppServerLogin()).resolves.toEqual(login)
    await expect(getCodexAppServerLogin('profile-1')).resolves.toMatchObject({ status: 'completed' })
    await expect(createCodexAppServerAccount('profile-1', { name: '个人官方运行时', priority: 1 })).resolves.toEqual({
      id: 9,
      name: '个人官方运行时'
    })
    await expect(cancelCodexAppServerLogin('profile-1')).resolves.toEqual({ status: 'cancelled' })

    expect(post).toHaveBeenNthCalledWith(1, '/admin/openai/app-server/login/start', { mode: 'device_code' }, { timeout: 90_000 })
    expect(get).toHaveBeenCalledWith('/admin/openai/app-server/login/profile-1')
    expect(post).toHaveBeenNthCalledWith(2, '/admin/openai/app-server/login/profile-1/create-account', {
      name: '个人官方运行时',
      priority: 1
    })
    expect(post).toHaveBeenNthCalledWith(3, '/admin/openai/app-server/login/profile-1/cancel')
    expect(JSON.stringify(post.mock.calls)).not.toContain('access_token')
    expect(JSON.stringify(post.mock.calls)).not.toContain('refresh_token')
  })
})

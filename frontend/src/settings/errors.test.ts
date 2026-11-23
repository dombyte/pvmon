import { describe, expect, it, vi } from 'vitest'
import { login } from '@/api/client'
import { formError } from './errors'

const t = (k: string) => `T:${k}`

async function failWith(status: number, body: unknown): Promise<unknown> {
  vi.stubGlobal(
    'fetch',
    vi.fn(async () => new Response(JSON.stringify(body), { status })),
  )
  try {
    await login('a', 'b')
  } catch (err) {
    return err
  } finally {
    vi.unstubAllGlobals()
  }
  throw new Error('no error')
}

describe('formError', () => {
  it('translates password reasons and joins problems per path', async () => {
    const err = await failWith(422, {
      code: 'validation_failed',
      message: 'x',
      details: [
        { path: 'password', message: 'too_short' },
        { path: 'site.interval', message: 'bad duration' },
        { path: 'site.interval', message: 'too small' },
        { path: 'password', message: 'odd' },
      ],
    })
    expect(formError(err, t)).toEqual({
      key: 'errors.validation_failed',
      fields: {
        password: 'T:password.too_short; odd',
        'site.interval': 'bad duration; too small',
      },
    })
  })

  it('has no fields for other errors', async () => {
    const err = await failWith(401, { code: 'unauthorized', message: 'x' })
    expect(formError(err, t)).toEqual({ key: 'errors.unauthorized', fields: {} })
    expect(formError(new Error('x'), t)).toEqual({ key: 'errors.unknown', fields: {} })
  })
})

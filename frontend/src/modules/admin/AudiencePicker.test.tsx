import { describe, expect, it } from 'vitest'
import { audienceValid } from './AudiencePicker'

// audienceValid mirrors push.Audience.Valid() on the server. It exists because
// the picker can produce an audience the server refuses — switch to "Podle role"
// and tick nothing — and the send button used to stay enabled, turning a state
// RecipientEcho had already flagged in red into a 422 error toast.
describe('audienceValid', () => {
  it('accepts "all", subscribers or not', () => {
    // "Všem" with nobody subscribed is the household's state, not a validation
    // failure, so it must stay sendable.
    expect(audienceValid({ scope: 'all' })).toBe(true)
  })

  it('refuses a role or user scope that names nobody', () => {
    expect(audienceValid({ scope: 'roles' })).toBe(false)
    expect(audienceValid({ scope: 'roles', roles: [] })).toBe(false)
    expect(audienceValid({ scope: 'users' })).toBe(false)
    expect(audienceValid({ scope: 'users', users: [] })).toBe(false)
  })

  it('accepts a role or user scope with a selection', () => {
    expect(audienceValid({ scope: 'roles', roles: ['admin'] })).toBe(true)
    expect(audienceValid({ scope: 'users', users: ['u-karel'] })).toBe(true)
  })

  it('ignores a stale selection carried by another scope', () => {
    // The picker keeps roles/users around when you switch scope, so "all" must
    // not be judged on a list it does not use.
    expect(audienceValid({ scope: 'all', roles: [], users: [] })).toBe(true)
  })
})

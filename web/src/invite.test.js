import { describe, it, expect } from 'vitest'
import { parseInviteCode, inviteURL } from './invite.js'

describe('invite', () => {
  it('parses /join/<code> and upper-cases it', () => {
    expect(parseInviteCode('/join/ab3d')).toBe('AB3D')
    expect(parseInviteCode('/join/AB3D/')).toBe('AB3D')
  })
  it('returns null for non-invite paths', () => {
    expect(parseInviteCode('/')).toBeNull()
    expect(parseInviteCode('/join/')).toBeNull()
    expect(parseInviteCode('')).toBeNull()
  })
  it('builds an invite URL', () => {
    expect(inviteURL('https://x.example', 'AB3D')).toBe('https://x.example/join/AB3D')
  })
})

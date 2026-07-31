export function parseInviteCode(pathname) {
  const m = /^\/join\/([A-Za-z0-9]+)\/?$/.exec(pathname || '')
  return m ? m[1].toUpperCase() : null
}

export function inviteURL(origin, code) {
  return `${origin}/join/${code}`
}

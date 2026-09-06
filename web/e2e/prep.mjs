// Builds everything the E2E run needs: the production frontend, embedded into
// the Go binary, compiled to a fixed temp path the Playwright webServer starts.
import { execSync } from 'node:child_process'
import { cpSync, rmSync, mkdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

const webDir = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repo = resolve(webDir, '..')
const run = (cmd, cwd) => execSync(cmd, { cwd, stdio: 'inherit' })

console.log('[e2e prep] building frontend…')
run('npm run build', webDir)

const embed = resolve(repo, 'internal/webui/dist')
console.log('[e2e prep] embedding dist…')
rmSync(embed, { recursive: true, force: true })
mkdirSync(embed, { recursive: true })
cpSync(resolve(webDir, 'dist'), embed, { recursive: true })

console.log('[e2e prep] compiling binary…')
run('go build -o /tmp/fauxlands-e2e-bin ./cmd/fauxtist', repo)
console.log('[e2e prep] ready.')

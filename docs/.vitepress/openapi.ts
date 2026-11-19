import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import type { Plugin } from 'vite'

// Serves web/openapi.json at /openapi.json for the API reference page, with
// the operations sorted into sections. The Go code does not tag operations, so
// the sections come from the path here; a new path lands in "Other" until it
// is added below.
const source = fileURLToPath(new URL('../../web/openapi.json', import.meta.url))

type Rule = [name: string, test: RegExp, about: string]

const rules: Rule[] = [
  ['Sign-in', /^\/auth\/(login|logout|me|options|forgot|reset|register|invite|sso|identities|password)/, 'Signing in and out, registration, password reset and single sign-on.'],
  ['Two-factor and passkeys', /^\/auth\/(totp|passkeys)/, 'Authenticator codes, recovery codes and passkeys.'],
  ['API keys and sessions', /^\/auth\/(keys|sessions)/, 'Your API keys and the places you are signed in.'],
  ['Power and console', /^\/servers\/\{uuid\}\/(power|command)$/, 'Start, stop, restart and kill a server, and send console commands. The live console itself is a websocket.'],
  ['Files', /^\/servers\/\{uuid\}\/files/, "Everything under a server's data directory. Paths are relative to it."],
  ['Backups', /^\/servers\/\{uuid\}\/backups/, 'Archives of a server’s files, stored on the node or in S3.'],
  ['Databases', /^\/servers\/\{uuid\}\/databases/, 'MySQL or MariaDB databases on the database hosts an admin added.'],
  ['Schedules', /^\/servers\/\{uuid\}\/schedules/, 'Timed tasks: power actions, commands and backups.'],
  ['Startup', /^\/servers\/\{uuid\}\/startup/, 'The startup command, Docker image and egg variables.'],
  ['Ports', /^\/servers\/\{uuid\}\/network/, 'The ports a server listens on.'],
  ['Mods', /^\/servers\/\{uuid\}\/mods/, 'Searching Modrinth and installing from it.'],
  ['Sharing access', /^(\/servers\/\{uuid\}\/(users|invites)|\/permissions)/, 'Subusers, their permissions, and invitations by email.'],
  ['Servers (admin)', /^\/servers\/\{uuid\}\/(limits|owner|suspend|unsuspend|external-id)$/, 'Changes only an admin can make to a server.'],
  ['Servers', /^\/servers/, 'Creating, listing and deleting servers, and what each one reports.'],
  ['Nodes', /^\/nodes/, 'The machines that run game servers.'],
  ['Eggs', /^\/eggs/, 'Server types, imported from Pterodactyl egg files.'],
  ['Accounts', /^\/(users|invites)/, 'Accounts on the panel and invitations to make one.'],
  ['Database hosts', /^\/database-hosts/, 'MySQL or MariaDB servers that server databases are made on.'],
  ['Webhooks', /^\/webhooks/, 'Signed event deliveries to your own address.'],
  ['Audit log', /^\/audit/, 'Every admin and security action on the panel.'],
]

const description = `Everything the panel's interface does goes through this API. Send an API key as \`Authorization: Bearer gp_...\`; create one under **Account → API keys**. The key acts as your account.

Errors are [RFC 9457 problem details](https://www.rfc-editor.org/rfc/rfc9457). Servers you cannot see answer \`404\`, exactly like servers that do not exist.

To try a request from this page, enter your panel's address below. Your panel must allow it: requests from other sites are refused unless they use an API key. The [API overview](/api/overview) has the details and common calls.`

// Section order in the reference: a server's own sections first, then your
// account, then what admins manage.
const order = [
  'Servers', 'Power and console', 'Files', 'Backups', 'Databases', 'Schedules', 'Startup', 'Ports', 'Mods', 'Sharing access',
  'Sign-in', 'Two-factor and passkeys', 'API keys and sessions',
  'Servers (admin)', 'Nodes', 'Eggs', 'Accounts', 'Database hosts', 'Webhooks', 'Audit log',
]

export function buildSpec(): string {
  const spec = JSON.parse(readFileSync(source, 'utf8'))
  const used = new Set<string>()
  for (const [path, ops] of Object.entries<Record<string, any>>(spec.paths)) {
    const rule = rules.find(([, test]) => test.test(path))
    const tag = rule?.[0] ?? 'Other'
    used.add(tag)
    for (const op of Object.values(ops)) op.tags = [tag]
  }
  const about = new Map(rules.map(([name, , text]) => [name, text]))
  spec.tags = order.filter((name) => used.has(name)).map((name) => ({ name, description: about.get(name) }))
  if (used.has('Other')) spec.tags.push({ name: 'Other' })
  spec.info.description = description
  spec.servers = [
    {
      url: '{panel}/api',
      description: 'Your panel',
      variables: { panel: { default: 'https://panel.example.com', description: "Your panel's address" } },
    },
  ]
  return JSON.stringify(spec)
}

export function openapi(): Plugin {
  let ssr = false
  return {
    name: 'gamepanel-openapi',
    configResolved(c) {
      ssr = !!c.build.ssr
    },
    configureServer(server) {
      server.watcher.add(source)
      server.middlewares.use('/openapi.json', (_req, res) => {
        res.setHeader('Content-Type', 'application/json')
        res.end(buildSpec())
      })
    },
    generateBundle() {
      if (!ssr) this.emitFile({ type: 'asset', fileName: 'openapi.json', source: buildSpec() })
    },
  }
}

import { defineConfig } from 'vitepress'
import llmstxt, { copyOrDownloadAsMarkdownButtons } from 'vitepress-plugin-llms'
import { openapi } from './openapi'

// The docs site. Pages live next to this folder as Markdown; asvs.md and
// server-creation.md keep their old paths because code comments link to them.
// DOCS_BASE is the path the site is served under: /gamepanel/ on GitHub Pages
// (.github/workflows/docs.yml), / everywhere else.
const base = process.env.DOCS_BASE ?? '/'

export default defineConfig({
  base,
  title: 'GamePanel',
  description: 'A self-hosted game server panel: one Go binary and one SQLite file, a small daemon per machine, and Pterodactyl egg import.',
  cleanUrls: true,
  lastUpdated: true,
  srcExclude: ['README.md'],
  vite: {
    plugins: [
      openapi(),
      // /llms.txt, /llms-full.txt and a Markdown copy of every page, for
      // people who read the docs through an AI assistant.
      llmstxt({
        title: 'GamePanel',
        description: 'A self-hosted game server panel: one Go binary and one SQLite file, a small daemon per machine, and Pterodactyl egg import.',
        details: 'Pre-alpha: not yet run in production. The API reference is the OpenAPI 3.1 description at /openapi.json.',
        ignoreFiles: ['index.md', 'api/reference.md'],
      }),
    ],
  },
  head: [
    ['link', { rel: 'icon', type: 'image/svg+xml', href: `${base}logo.svg` }],
    ['meta', { name: 'theme-color', content: '#0b0b0a' }],
  ],
  markdown: {
    theme: { light: 'vitesse-light', dark: 'vesper' },
    // Pages quote Pterodactyl's {{VAR}} placeholders in inline code. Fenced
    // blocks are already left alone by Vue; do the same for inline code.
    config(md) {
      md.use(copyOrDownloadAsMarkdownButtons, { componentName: 'PageActions', showDownload: false })
      const inline = md.renderer.rules.code_inline!
      md.renderer.rules.code_inline = (tokens, idx, opts, env, self) =>
        inline(tokens, idx, opts, env, self).replace(/^<code/, '<code v-pre')
      // "- [ ] item" becomes a checklist row (the production checklist uses it).
      md.core.ruler.push('task_list', (state) => {
        state.tokens.forEach((t, i) => {
          if (t.type !== 'inline' || state.tokens[i - 2]?.type !== 'list_item_open') return
          const first = t.children?.[0]
          if (!first || first.type !== 'text' || !/^\[[ x]\] /.test(first.content)) return
          const done = first.content[1] === 'x'
          first.content = first.content.slice(4)
          const box = new state.Token('html_inline', '', 0)
          box.content = `<span class="task${done ? ' done' : ''}" aria-hidden="true"></span>`
          t.children!.unshift(box)
          state.tokens[i - 2].attrJoin('class', 'task-item')
        })
      })
    },
  },
  themeConfig: {
    logo: { light: '/logo-light.svg', dark: '/logo.svg', width: 24, height: 24, alt: '' },
    nav: [
      { text: 'Guide', link: '/getting-started/introduction' },
      { text: 'Install', link: '/panel/installing' },
      { text: 'API', link: '/api/reference', activeMatch: '^/api/' },
      { text: 'Troubleshooting', link: '/troubleshooting' },
    ],
    sidebar: [
      {
        text: 'Getting started',
        items: [
          { text: 'Introduction', link: '/getting-started/introduction' },
          { text: 'How it works', link: '/getting-started/architecture' },
          { text: 'Requirements', link: '/getting-started/requirements' },
          { text: 'Try it locally', link: '/getting-started/quick-start' },
          { text: 'Production checklist', link: '/getting-started/production-checklist' },
        ],
      },
      {
        text: 'Panel',
        items: [
          { text: 'Installing the panel', link: '/panel/installing' },
          { text: 'Docker Compose', link: '/panel/docker' },
          { text: 'Reverse proxy and HTTPS', link: '/panel/reverse-proxy' },
          { text: 'Configuration reference', link: '/panel/configuration' },
          { text: 'Email', link: '/panel/email' },
          { text: 'Single sign-on', link: '/panel/sso' },
          { text: 'Backups in S3', link: '/panel/s3-backups' },
          { text: 'Command line', link: '/panel/cli' },
          { text: 'Backing up the panel', link: '/panel/backing-up' },
          { text: 'Upgrading', link: '/panel/upgrading' },
          { text: 'Repair, settings, uninstall', link: '/panel/managing' },
        ],
      },
      {
        text: 'Nodes',
        items: [
          { text: 'Installing a node', link: '/nodes/installing' },
          { text: 'Configuration reference', link: '/nodes/configuration' },
          { text: 'SFTP', link: '/nodes/sftp' },
          { text: 'Network limits', link: '/nodes/network-limits' },
          { text: 'Data directory', link: '/nodes/data-directory' },
        ],
      },
      {
        text: 'Administration',
        items: [
          { text: 'Nodes and capacity', link: '/admin/nodes' },
          { text: 'Eggs', link: '/admin/eggs' },
          { text: 'Minecraft eggs', link: '/admin/minecraft-eggs' },
          { text: 'Creating servers', link: '/server-creation' },
          { text: 'Accounts and sign-up', link: '/admin/accounts' },
          { text: 'Database hosts', link: '/admin/database-hosts' },
          { text: 'Suspension', link: '/admin/suspension' },
          { text: 'Audit log', link: '/admin/audit-log' },
        ],
      },
      {
        text: 'Using servers',
        items: [
          { text: 'Console and power', link: '/servers/console-and-power' },
          { text: 'Files', link: '/servers/files' },
          { text: 'Mods and plugins', link: '/servers/mods' },
          { text: 'Startup settings', link: '/servers/startup' },
          { text: 'Ports', link: '/servers/network' },
          { text: 'Databases', link: '/servers/databases' },
          { text: 'Backups', link: '/servers/backups' },
          { text: 'Schedules', link: '/servers/schedules' },
          { text: 'Sharing access', link: '/servers/subusers' },
          { text: 'Activity', link: '/servers/activity' },
        ],
      },
      {
        text: 'Your account',
        items: [{ text: 'Sign-in and security', link: '/account/security' }],
      },
      {
        text: 'API and integrations',
        items: [
          { text: 'API overview', link: '/api/overview' },
          { text: 'API reference', link: '/api/reference' },
          { text: 'Shops and billing', link: '/server-creation#selling-servers-the-shop-api' },
          { text: 'Webhooks', link: '/server-creation#webhooks' },
          { text: 'Pterodactyl API', link: '/api/pterodactyl' },
        ],
      },
      {
        text: 'Guides',
        items: [
          { text: 'Compared with other panels', link: '/guides/compare' },
          { text: 'Coming from Pterodactyl', link: '/guides/from-pterodactyl' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'Troubleshooting', link: '/troubleshooting' },
          { text: 'Security model', link: '/security/model' },
          { text: 'Account security and ASVS', link: '/asvs' },
          { text: 'Limits and timeouts', link: '/reference/limits' },
          { text: 'Development', link: '/reference/development' },
        ],
      },
    ],
    search: { provider: 'local' },
    editLink: {
      pattern: 'https://github.com/ZsombiiHUN/gamepanel/edit/main/docs/:path',
      text: 'Edit this page on GitHub',
    },
    socialLinks: [{ icon: 'github', link: 'https://github.com/ZsombiiHUN/gamepanel' }],
    footer: { message: 'Released under the AGPL-3.0 license.' },
    outline: { level: [2, 3] },
  },
})

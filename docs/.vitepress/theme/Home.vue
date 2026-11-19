<script setup lang="ts">
import { withBase } from 'vitepress'
import SystemMap from './SystemMap.vue'

type Entry = { text: string; link: string; note: string }

const sections: { title: string; who: string; items: Entry[] }[] = [
  {
    title: 'Run it',
    who: 'For whoever owns the machines',
    items: [
      { text: 'Installing the panel', link: '/panel/installing', note: 'systemd, a service user, the first admin' },
      { text: 'Reverse proxy and HTTPS', link: '/panel/reverse-proxy', note: 'Caddy, nginx, Cloudflare' },
      { text: 'Installing a node', link: '/nodes/installing', note: 'Docker, the daemon, the firewall' },
      { text: 'Backing up the panel', link: '/panel/backing-up', note: 'the database and its key file' },
      { text: 'Upgrading', link: '/panel/upgrading', note: 'and going back' },
    ],
  },
  {
    title: 'Administer',
    who: 'For panel admins',
    items: [
      { text: 'Eggs', link: '/admin/eggs', note: 'what imports from Pterodactyl and what does not' },
      { text: 'Creating servers', link: '/server-creation', note: 'by hand, by API, from a shop' },
      { text: 'Accounts and sign-up', link: '/admin/accounts', note: 'invites, roles, lockouts' },
      { text: 'Database hosts', link: '/admin/database-hosts', note: 'MySQL for game servers' },
      { text: 'Audit log', link: '/admin/audit-log', note: 'every event name' },
    ],
  },
  {
    title: 'Use a server',
    who: 'For server owners and their friends',
    items: [
      { text: 'Console and power', link: '/servers/console-and-power', note: 'start, stop, limits' },
      { text: 'Files', link: '/servers/files', note: 'editor, uploads, SFTP' },
      { text: 'Mods and plugins', link: '/servers/mods', note: 'install from Modrinth' },
      { text: 'Backups', link: '/servers/backups', note: 'make, restore, download' },
      { text: 'Sharing access', link: '/servers/subusers', note: 'subusers and permissions' },
    ],
  },
  {
    title: 'Build on it',
    who: 'For developers',
    items: [
      { text: 'API overview', link: '/api/overview', note: 'keys, OpenAPI, errors' },
      { text: 'Shops and billing', link: '/server-creation#selling-servers-the-shop-api', note: 'create servers on payment' },
      { text: 'Webhooks', link: '/server-creation#webhooks', note: 'signed events, retries' },
      { text: 'Pterodactyl client API', link: '/api/pterodactyl', note: 'the routes that work as they are' },
      { text: 'Development', link: '/reference/development', note: 'build, run, test' },
    ],
  },
]
</script>

<template>
  <div class="home">
    <header class="intro">
      <div class="lead">
        <a class="status" :href="withBase('/getting-started/production-checklist')">
          <span class="dot" aria-hidden="true" />
          <span>Pre-alpha</span>
          <span class="sep" aria-hidden="true" />
          <span class="muted">not yet run in production. Read the checklist first</span>
        </a>
        <h1>Run game servers on machines you own.</h1>
        <p>
          The panel is one Go binary and one SQLite file. Each machine runs a small daemon that dials out to the
          panel, so nodes need no open control port. It imports Pterodactyl eggs.
        </p>
        <div class="actions">
          <a class="btn primary" :href="withBase('/getting-started/quick-start')">Try it locally</a>
          <a class="btn" :href="withBase('/panel/installing')">Install on a server</a>
          <a class="link" :href="withBase('/guides/from-pterodactyl')">Coming from Pterodactyl</a>
        </div>
      </div>

      <div class="term" aria-label="Quick start commands">
        <div class="term-bar">
          <span>Try it locally</span>
          <span class="muted">no Docker needed</span>
        </div>
        <pre><code><span class="c"># build the interface, then bin/panel and bin/daemon</span>
make web build

<span class="c"># the panel, on http://127.0.0.1:8080</span>
export GAMEPANEL_DB=./panel.db
bin/panel create-admin you@example.com
bin/panel

<span class="c"># a node, with a token from Nodes → Create node</span>
GAMEPANEL_NODE_TOKEN=<span class="v">&lt;token&gt;</span> \
GAMEPANEL_RUNTIME=fake bin/daemon</code></pre>
      </div>
    </header>

    <section class="parts">
      <div class="parts-text">
        <h2>How the parts fit</h2>
        <p>Three things to know before you install it.</p>
        <dl>
          <dt>The panel holds everything</dt>
          <dd>Accounts, servers, schedules and settings live in one database file. Back up that file and its key and you have backed up the panel.</dd>
          <dt>Nodes call the panel</dt>
          <dd>The daemon opens one websocket to the panel and keeps it open. Servers keep running if it drops.</dd>
          <dt>One container per server</dt>
          <dd>Each game server runs in its own Docker container with dropped capabilities and its files on the node's disk.</dd>
        </dl>
        <a class="more" :href="withBase('/getting-started/architecture')">How it works in detail</a>
      </div>
      <SystemMap />
    </section>

    <section class="index">
      <h2>Find your way</h2>
      <div class="cols">
        <nav v-for="s in sections" :key="s.title" :aria-label="s.title">
          <h3>{{ s.title }}</h3>
          <p class="who">{{ s.who }}</p>
          <ul>
            <li v-for="i in s.items" :key="i.link">
              <a :href="withBase(i.link)">
                <span class="name">{{ i.text }}</span>
                <span class="note">{{ i.note }}</span>
              </a>
            </li>
          </ul>
        </nav>
      </div>
    </section>

    <section class="more-row">
      <a :href="withBase('/troubleshooting')"><span>Troubleshooting</span><span class="note">by symptom</span></a>
      <a :href="withBase('/reference/limits')"><span>Limits and timeouts</span><span class="note">every fixed number</span></a>
      <a :href="withBase('/security/model')"><span>Security model</span><span class="note">what is trusted, and why</span></a>
      <a :href="withBase('/getting-started/production-checklist#known-gaps')"><span>Known gaps</span><span class="note">what is not built yet</span></a>
    </section>
  </div>
</template>

<style scoped>
.home {
  max-width: 1152px;
  margin: 0 auto;
  padding: 56px 24px 96px;
  color: var(--gp-text);
}

@media (min-width: 768px) {
  .home {
    padding: 80px 48px 120px;
  }
}

/* ---------- intro ---------- */

.intro {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 48px;
  align-items: start;
}

@media (min-width: 1024px) {
  .intro {
    grid-template-columns: minmax(0, 1fr) minmax(0, 480px);
    gap: 64px;
  }
}

.status {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 4px 12px 4px 10px;
  border: 1px solid var(--gp-line);
  border-radius: 99px;
  font-size: 13px;
  font-weight: 500;
  color: var(--gp-text);
  text-decoration: none;
  transition: border-color 0.15s;
}

.status:hover {
  border-color: var(--gp-line-strong);
}

.status > span:nth-child(2) {
  white-space: nowrap;
}

@media (max-width: 520px) {
  .status .sep,
  .status .muted {
    display: none;
  }
}

.status .dot {
  width: 7px;
  height: 7px;
  border-radius: 99px;
  background: var(--gp-warn);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--gp-warn) 20%, transparent);
}

.status .sep {
  width: 1px;
  height: 12px;
  background: var(--gp-line-strong);
}

.muted {
  color: var(--gp-faint);
  font-weight: 450;
}

h1 {
  margin: 28px 0 0;
  max-width: 14ch;
  font-size: clamp(36px, 5.2vw, 56px);
  line-height: 1.04;
  font-weight: 600;
  letter-spacing: -0.035em;
}

.lead > p {
  margin: 20px 0 0;
  max-width: 52ch;
  font-size: 17px;
  line-height: 1.6;
  color: var(--gp-muted);
}

.actions {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px 12px;
  margin-top: 32px;
}

.btn {
  display: inline-flex;
  align-items: center;
  height: 38px;
  padding: 0 16px;
  border: 1px solid var(--gp-line-strong);
  border-radius: 8px;
  background: var(--gp-panel);
  font-size: 14px;
  font-weight: 500;
  color: var(--gp-text);
  text-decoration: none;
  transition: background-color 0.15s, border-color 0.15s;
}

.btn:hover {
  background: var(--gp-chip);
}

.btn.primary {
  border-color: transparent;
  background: var(--gp-text);
  color: var(--gp-panel);
}

.btn.primary:hover {
  background: var(--gp-muted);
}

.link {
  margin-left: 4px;
  font-size: 14px;
  font-weight: 450;
  color: var(--gp-muted);
  text-decoration: underline;
  text-decoration-color: var(--gp-line-strong);
  text-underline-offset: 3px;
}

.link:hover {
  color: var(--gp-text);
  text-decoration-color: currentColor;
}

/* The quick start, as a terminal. */
.term {
  border: 1px solid var(--gp-line);
  border-radius: 12px;
  background: var(--gp-well);
  overflow: hidden;
}

.term-bar {
  display: flex;
  justify-content: space-between;
  padding: 10px 16px;
  border-bottom: 1px solid var(--gp-line);
  font-size: 12.5px;
  font-weight: 500;
}

.term pre {
  margin: 0;
  padding: 16px 18px 18px;
  overflow-x: auto;
  font-family: var(--vp-font-family-mono);
  font-size: 13px;
  line-height: 1.75;
  color: var(--gp-text);
}

.term .c {
  color: var(--gp-faint);
}

.term .v {
  color: var(--gp-muted);
}

/* ---------- parts ---------- */

section {
  margin-top: 96px;
}

h2 {
  margin: 0;
  font-size: 24px;
  font-weight: 600;
  letter-spacing: -0.02em;
}

.parts {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 32px;
  align-items: center;
  padding-top: 48px;
  border-top: 1px solid var(--gp-line);
}

@media (min-width: 1024px) {
  .parts {
    grid-template-columns: minmax(0, 340px) minmax(0, 1fr);
    gap: 64px;
  }
}

.parts-text > p {
  margin: 8px 0 0;
  color: var(--gp-muted);
  font-size: 15px;
}

dl {
  margin: 24px 0 0;
}

dt {
  margin-top: 18px;
  font-size: 14.5px;
  font-weight: 600;
}

dd {
  margin: 4px 0 0;
  font-size: 14.5px;
  line-height: 1.6;
  color: var(--gp-muted);
}

.more {
  display: inline-block;
  margin-top: 24px;
  font-size: 14px;
  font-weight: 500;
  color: var(--gp-text);
  text-decoration: underline;
  text-decoration-color: var(--gp-line-strong);
  text-underline-offset: 3px;
}

.more:hover {
  text-decoration-color: currentColor;
}

/* ---------- index ---------- */

.index {
  padding-top: 48px;
  border-top: 1px solid var(--gp-line);
}

.cols {
  display: grid;
  gap: 40px 32px;
  margin-top: 32px;
}

@media (min-width: 640px) {
  .cols {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (min-width: 1024px) {
  .cols {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

h3 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}

.who {
  margin: 2px 0 0;
  font-size: 13px;
  color: var(--gp-faint);
}

ul {
  margin: 14px 0 0;
  padding: 0;
  list-style: none;
  border-top: 1px solid var(--gp-line);
}

li a {
  display: block;
  padding: 10px 0;
  border-bottom: 1px solid var(--gp-line);
  text-decoration: none;
}

.name {
  display: block;
  font-size: 14px;
  font-weight: 500;
  color: var(--gp-text);
}

.note {
  display: block;
  margin-top: 1px;
  font-size: 13px;
  color: var(--gp-faint);
}

li a:hover .name {
  text-decoration: underline;
  text-decoration-color: var(--gp-line-strong);
  text-underline-offset: 3px;
}

/* ---------- more ---------- */

.more-row {
  display: grid;
  margin-top: 64px;
  border: 1px solid var(--gp-line);
  border-radius: 12px;
  overflow: hidden;
}

@media (min-width: 640px) {
  .more-row {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (min-width: 1024px) {
  .more-row {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

.more-row a {
  padding: 16px 20px;
  font-size: 14px;
  font-weight: 500;
  color: var(--gp-text);
  text-decoration: none;
  box-shadow: -1px -1px 0 var(--gp-line);
  transition: background-color 0.15s;
}

.more-row a:hover {
  background: var(--gp-well);
}
</style>

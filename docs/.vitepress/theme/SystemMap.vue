<script setup lang="ts">
// The parts of an installation and what talks to what. Drawn from
// getting-started/architecture.md; keep the two in step.
</script>

<template>
  <figure class="map">
    <div class="scroll">
      <svg viewBox="0 0 760 300" role="img" aria-labelledby="map-title map-desc">
        <title id="map-title">How GamePanel's parts connect</title>
        <desc id="map-desc">
          A browser reaches the panel over HTTPS through a reverse proxy. Each node's daemon dials out to the
          panel over a secure websocket and runs every game server in its own Docker container. SFTP clients
          connect to the daemon on port 2022; players connect straight to the containers on their game ports.
        </desc>
        <defs>
          <marker id="arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
            <path d="M0 0.5 L7 4 L0 7.5" class="head" />
          </marker>
        </defs>

        <!-- who connects -->
        <g class="actor">
          <rect x="16" y="62" width="112" height="46" rx="8" />
          <text x="72" y="90" text-anchor="middle">Browser</text>
          <rect x="16" y="150" width="112" height="40" rx="8" />
          <text x="72" y="175" text-anchor="middle">SFTP client</text>
          <rect x="16" y="214" width="112" height="40" rx="8" />
          <text x="72" y="239" text-anchor="middle">Players</text>
        </g>

        <!-- proxy -->
        <g class="box">
          <rect x="172" y="62" width="118" height="46" rx="8" />
          <text x="231" y="83" text-anchor="middle" class="t">Caddy / nginx</text>
          <text x="231" y="99" text-anchor="middle" class="s">HTTPS</text>
        </g>

        <!-- panel -->
        <g class="box main">
          <rect x="332" y="30" width="160" height="110" rx="10" />
          <text x="350" y="56" class="t">Panel</text>
          <text x="350" y="80" class="s">one Go binary</text>
          <text x="350" y="99" class="s">web UI and API</text>
          <text x="350" y="118" class="s">SQLite + key file</text>
        </g>

        <!-- node -->
        <g class="node">
          <rect x="580" y="14" width="170" height="272" rx="12" />
          <text x="596" y="36" class="label">NODE</text>
        </g>
        <g class="box main">
          <rect x="596" y="48" width="138" height="82" rx="10" />
          <circle cx="614" cy="70" r="3.5" class="ok" />
          <text x="625" y="74" class="t">Daemon</text>
          <text x="610" y="96" class="s">servers, files,</text>
          <text x="610" y="114" class="s">backups, SFTP</text>
        </g>
        <g class="box">
          <rect x="596" y="166" width="138" height="102" rx="10" />
          <text x="610" y="188" class="t">Docker</text>
          <rect x="610" y="202" width="110" height="24" rx="5" class="ctr" />
          <text x="620" y="218" class="s">game server</text>
          <rect x="610" y="234" width="110" height="24" rx="5" class="ctr" />
          <text x="620" y="250" class="s">game server</text>
        </g>

        <!-- links -->
        <g class="wire">
          <path d="M128 85 H170" marker-end="url(#arrow)" />
          <path d="M290 85 H330" marker-end="url(#arrow)" />
          <path d="M596 85 H494" marker-end="url(#arrow)" />
          <path d="M665 130 V164" marker-end="url(#arrow)" />
          <path d="M128 170 H560 V118 H594" marker-end="url(#arrow)" />
          <path d="M128 234 H594" marker-end="url(#arrow)" />
        </g>
        <g class="wire-label">
          <text x="544" y="77" text-anchor="middle">wss</text>
          <text x="544" y="103" text-anchor="middle" class="dim">dials out</text>
          <text x="146" y="162" class="dim">SFTP on port 2022, login checked with the panel</text>
          <text x="146" y="226" class="dim">game ports, straight to the server</text>
        </g>
      </svg>
    </div>
  </figure>
</template>

<style scoped>
.map {
  margin: 24px 0;
}

.scroll {
  overflow-x: auto;
}

svg {
  display: block;
  width: 100%;
  min-width: 600px;
  height: auto;
  font-family: var(--vp-font-family-mono);
}

.actor rect {
  fill: transparent;
  stroke: var(--gp-line-strong);
  stroke-dasharray: 3 3;
}

.actor text {
  fill: var(--gp-muted);
  font-size: 12.5px;
}

.box rect {
  fill: var(--gp-well);
  stroke: var(--gp-line-strong);
}

.box.main rect {
  fill: var(--gp-panel);
}

.box .ctr {
  fill: var(--gp-chip);
  stroke: var(--gp-line);
}

.t {
  fill: var(--gp-text);
  font-family: var(--vp-font-family-base);
  font-size: 14px;
  font-weight: 600;
}

.s {
  fill: var(--gp-muted);
  font-size: 11.5px;
}

.node rect {
  fill: var(--gp-side);
  stroke: var(--gp-line);
}

.node .label {
  fill: var(--gp-faint);
  font-size: 10.5px;
  letter-spacing: 0.08em;
}

.ok {
  fill: var(--gp-good);
}

.wire path {
  fill: none;
  stroke: var(--gp-faint);
  stroke-width: 1.25;
}

.head {
  fill: none;
  stroke: var(--gp-faint);
  stroke-width: 1.25;
  stroke-linejoin: round;
}

.wire-label text {
  fill: var(--gp-text);
  font-size: 11px;
}

.wire-label .dim {
  fill: var(--gp-faint);
}
</style>

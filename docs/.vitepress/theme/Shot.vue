<script setup lang="ts">
// A screenshot of the panel that follows the site's theme: <Shot name="console" alt="..." />
// shows public/screenshots/console-dark.webp or console-light.webp. To retake
// them, run the e2e test panel (internal/e2e/testpanel) and capture 1440×900 at 2×.
import { withBase } from 'vitepress'

defineProps<{ name: string; alt: string }>()
</script>

<template>
  <figure class="shot">
    <img class="for-dark" :src="withBase(`/screenshots/${name}-dark.webp`)" :alt="alt" width="1440" height="900" loading="lazy" decoding="async" />
    <img class="for-light" :src="withBase(`/screenshots/${name}-light.webp`)" :alt="alt" width="1440" height="900" loading="lazy" decoding="async" />
  </figure>
</template>

<style scoped>
.shot {
  margin: 20px 0 28px;
  border: 1px solid var(--gp-line);
  border-radius: 10px;
  overflow: hidden;
  background: var(--gp-panel);
}

img {
  display: block;
  width: 100%;
  height: auto;
}
</style>

<style>
/* Unscoped: a scoped :global(.dark) would swallow the rest of the selector. */
.shot .for-dark {
  display: none;
}

.dark .shot .for-dark {
  display: block;
}

.dark .shot .for-light {
  display: none;
}
</style>

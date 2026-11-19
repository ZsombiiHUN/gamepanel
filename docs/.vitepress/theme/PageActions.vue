<script setup lang="ts">
// Shown under each page's title. Every page is also published as Markdown
// (page.md), and the whole site as /llms.txt and /llms-full.txt, by
// vitepress-plugin-llms; these buttons hand that Markdown on.
import { useCopyOrDownloadAsMarkdownButtons } from 'vitepress-plugin-llms/vitepress-components'

const { aiProviders, copied, copyAsMarkdown, viewAsMarkdown, openInAI } = useCopyOrDownloadAsMarkdownButtons()
</script>

<template>
  <div class="page-actions">
    <button type="button" @click="copyAsMarkdown">
      <span class="dot" :class="{ on: copied }" aria-hidden="true"></span>
      <span aria-live="polite">{{ copied ? 'Copied' : 'Copy page' }}</span>
    </button>
    <button type="button" @click="viewAsMarkdown">View as Markdown</button>
    <button v-for="p in aiProviders" :key="p.name" type="button" @click="openInAI(p)">Ask {{ p.name }}</button>
  </div>
</template>

<style scoped>
.page-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 10px 0 28px;
}

button {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--gp-line);
  border-radius: 7px;
  background: transparent;
  color: var(--gp-muted);
  font-family: var(--vp-font-family-base);
  font-size: 12.5px;
  font-weight: 500;
  transition:
    color 0.15s,
    border-color 0.15s;
}

button:hover {
  color: var(--gp-text);
  border-color: var(--gp-line-strong);
}

button:focus-visible {
  outline: 2px solid var(--gp-text);
  outline-offset: 1px;
}

.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--gp-faint);
  transition: background 0.15s;
}

.dot.on {
  background: var(--gp-good);
}
</style>

<script setup lang="ts">
// The full API reference, drawn by Scalar from /openapi.json (see
// ../openapi.ts). Scalar only runs in the browser, so it loads on mount.
import { defineAsyncComponent, computed, h } from 'vue'
import { useData, withBase } from 'vitepress'

const Scalar = defineAsyncComponent({
  loader: async () => {
    await import('@scalar/api-reference/style.css')
    return (await import('@scalar/api-reference')).ApiReference
  },
  loadingComponent: () => h('p', { class: 'api-loading' }, 'Loading the API reference…'),
  delay: 100,
})

const { isDark } = useData()

const configuration = computed(() => ({
  url: withBase('/openapi.json'),
  theme: 'none',
  layout: 'modern',
  withDefaultFonts: false,
  forceDarkModeState: isDark.value ? 'dark' : 'light',
  hideDarkModeToggle: true,
  hideClientButton: false,
  showDeveloperTools: 'never',
  telemetry: false,
  // Scalar's "Ask AI" chat sends the spec to Scalar's hosted service; leave it off.
  agent: { disabled: true },
  defaultHttpClient: { targetKey: 'shell', clientKey: 'curl' },
  authentication: { preferredSecurityScheme: 'apikey' },
  persistAuth: false,
  documentDownloadType: 'json',
  orderRequiredPropertiesFirst: true,
}))
</script>

<template>
  <!-- vp-raw: keeps VitePress's router and doc styles away from Scalar's own links and markup -->
  <div class="api-ref vp-raw">
    <ClientOnly>
      <Scalar :configuration="configuration" />
    </ClientOnly>
  </div>
</template>

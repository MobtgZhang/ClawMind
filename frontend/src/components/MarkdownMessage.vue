<script setup lang="ts">
import { computed, ref } from "vue";
import MarkdownIt from "markdown-it";
import mdMultimdTable from "markdown-it-multimd-table";
import hljs from "highlight.js";
import "highlight.js/styles/github-dark.css";

const props = defineProps<{ source: string }>();
const root = ref<HTMLElement | null>(null);

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function highlightCodeInner(str: string, lang: string): string {
  if (lang && hljs.getLanguage(lang)) {
    try {
      return hljs.highlight(str, { language: lang, ignoreIllegals: true }).value;
    } catch {
      /* fall through */
    }
  }
  return escapeHtml(str);
}

const copyBtnSvg = `<button type="button" data-copy-code class="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-md text-ink-muted transition hover:bg-ink/10 hover:text-ink" title="复制" aria-label="复制">
  <svg class="copy-icon h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" /></svg>
  <svg class="check-icon hidden h-4 w-4 text-emerald-600 dark:text-emerald-400" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" /></svg>
</button>`;

function renderCodeBlock(lang: string, code: string): string {
  const inner = highlightCodeInner(code, lang);
  const langLabel = escapeHtml(lang || "text");
  return `<div class="md-code-wrap my-2 overflow-hidden rounded-lg border border-ink/12 bg-ink/[0.03] dark:border-ink/20 dark:bg-white/[0.04]">
  <div class="flex items-center justify-between gap-2 border-b border-ink/10 px-2 py-1 text-xs">
    <span class="font-mono text-ink-muted">${langLabel}</span>
    ${copyBtnSvg}
  </div>
  <pre class="hljs m-0 overflow-x-auto rounded-none border-0 bg-transparent p-3 text-[13px] leading-relaxed"><code>${inner}</code></pre>
</div>`;
}

const md = new MarkdownIt({
  html: false,
  linkify: true,
  breaks: true,
  highlight: () => "",
}).use(mdMultimdTable);

md.renderer.rules.fence = function (tokens, idx) {
  const token = tokens[idx];
  const info = token.info ? token.info.trim() : "";
  const lang = info.split(/\s+/g)[0] || "";
  return renderCodeBlock(lang, token.content);
};

const html = computed(() => md.render(props.source || ""));

function onRootClick(e: MouseEvent) {
  const btn = (e.target as HTMLElement).closest("[data-copy-code]");
  if (!btn || !root.value?.contains(btn)) return;
  const wrap = btn.closest(".md-code-wrap");
  const codeEl = wrap?.querySelector("pre code");
  if (!codeEl) return;
  const text = codeEl.textContent ?? "";
  void navigator.clipboard.writeText(text).then(() => {
    const copyIc = btn.querySelector(".copy-icon");
    const checkIc = btn.querySelector(".check-icon");
    if (copyIc && checkIc) {
      copyIc.classList.add("hidden");
      checkIc.classList.remove("hidden");
      window.setTimeout(() => {
        copyIc.classList.remove("hidden");
        checkIc.classList.add("hidden");
      }, 1400);
    }
  });
}
</script>

<template>
  <div
    ref="root"
    class="md-root max-w-none overflow-x-auto text-[15px] leading-relaxed text-ink [&_a]:text-accent [&_a]:underline [&_blockquote]:my-3 [&_blockquote]:border-l-4 [&_blockquote]:border-ink/20 [&_blockquote]:pl-4 [&_blockquote]:text-ink-muted [&_h1]:mb-2 [&_h1]:mt-4 [&_h1]:text-xl [&_h1]:font-semibold [&_h2]:mb-2 [&_h2]:mt-3 [&_h2]:text-lg [&_h2]:font-semibold [&_h3]:mb-1 [&_h3]:mt-2 [&_h3]:text-base [&_h3]:font-semibold [&_li]:my-1 [&_ol]:my-3 [&_ol]:list-decimal [&_ol]:pl-6 [&_p]:my-2 [&_p]:first:mt-0 [&_p]:last:mb-0 [&_ul]:my-3 [&_ul]:list-disc [&_ul]:pl-6 [&_code]:rounded [&_code]:bg-ink/10 [&_code]:px-1 [&_code]:py-0.5 [&_code]:text-[13px] [&_pre_code]:bg-transparent [&_pre_code]:p-0 [&_table]:my-4 [&_table]:w-full [&_table]:table-auto [&_table]:border-collapse [&_table]:border [&_table]:border-ink/15 [&_table]:text-[14px] dark:[&_table]:border-ink/25 [&_thead]:bg-ink/[0.045] dark:[&_thead]:bg-white/[0.06] [&_th]:border-b [&_th]:border-ink/15 [&_th]:px-3 [&_th]:py-2.5 [&_th]:text-left [&_th]:font-semibold [&_th]:text-ink dark:[&_th]:border-ink/25 [&_td]:border-b [&_td]:border-ink/12 [&_td]:px-3 [&_td]:py-2.5 [&_td]:align-top [&_td]:text-ink dark:[&_td]:border-ink/20 [&_tbody_tr:last-child_td]:border-b-0 [&_tbody_tr:nth-child(even)]:bg-ink/[0.02] dark:[&_tbody_tr:nth-child(even)]:bg-white/[0.03]"
    @click="onRootClick"
    v-html="html"
  />
</template>

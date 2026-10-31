<script setup lang="ts">
import { computed, ref } from "vue";
import MarkdownIt from "markdown-it";
import mdMultimdTable from "markdown-it-multimd-table";
import mdTexmath from "markdown-it-texmath";
import katex from "katex";
import "katex/dist/katex.min.css";
import "markdown-it-texmath/css/texmath.css";
import hljs from "highlight.js";
import "@/styles/hljs-maya.css";

const props = defineProps<{ source: string }>();
const root = ref<HTMLElement | null>(null);

/** 语言 → 下载扩展名（Maya 的 CODE_EXT 表） */
const CODE_EXT: Record<string, string> = {
  javascript: "js",
  js: "js",
  jsx: "jsx",
  typescript: "ts",
  ts: "ts",
  tsx: "tsx",
  vue: "vue",
  python: "py",
  py: "py",
  go: "go",
  golang: "go",
  rust: "rs",
  c: "c",
  h: "h",
  cpp: "cpp",
  "c++": "cpp",
  cc: "cpp",
  hpp: "hpp",
  java: "java",
  kotlin: "kt",
  swift: "swift",
  ruby: "rb",
  php: "php",
  shell: "sh",
  bash: "sh",
  sh: "sh",
  zsh: "sh",
  powershell: "ps1",
  sql: "sql",
  html: "html",
  css: "css",
  scss: "scss",
  json: "json",
  yaml: "yaml",
  yml: "yaml",
  toml: "toml",
  xml: "xml",
  markdown: "md",
  md: "md",
  diff: "diff",
  patch: "patch",
  dockerfile: "dockerfile",
  makefile: "mk",
  lua: "lua",
  zig: "zig",
  haskell: "hs",
  scala: "scala",
  r: "r",
  matlab: "m",
  julia: "jl",
};

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

function renderCodeBlock(lang: string, code: string): string {
  const inner = highlightCodeInner(code, lang);
  const langLabel = escapeHtml(lang || "text");
  return `<div class="code" data-lang="${langLabel}">
  <div class="code__head">
    <span class="code__lang">${langLabel}</span>
    <span class="code__actions">
      <button type="button" data-code-wrap class="code__btn" title="自动换行" aria-label="自动换行">
        <svg class="st-icon st-icon--sm" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m16 16-3 3 3 3"></path><path d="M3 12h14.5a1 1 0 0 1 0 7H13"></path><path d="M3 19h6"></path><path d="M3 5h18"></path></svg>
      </button>
      <button type="button" data-code-download class="code__btn" title="下载" aria-label="下载">
        <svg class="st-icon st-icon--sm" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 15V3"></path><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"></path><path d="m7 10 5 5 5-5"></path></svg>
      </button>
      <button type="button" data-code-copy class="code__btn" title="复制" aria-label="复制">
        <svg class="copy-icon st-icon st-icon--sm" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect width="14" height="14" x="8" y="8" rx="2" ry="2"></rect><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"></path></svg>
        <svg class="check-icon hidden st-icon st-icon--sm" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 6 9 17l-5-5"></path></svg>
      </button>
    </span>
  </div>
  <pre><code class="hljs">${inner}</code></pre>
</div>`;
}

const md = new MarkdownIt({
  html: false,
  linkify: true,
  breaks: true,
  highlight: () => "",
})
  .use(mdMultimdTable)
  .use(mdTexmath, {
    engine: katex,
    delimiters: "dollars",
    katexOptions: { throwOnError: false },
  });

md.renderer.rules.fence = function (tokens, idx) {
  const token = tokens[idx];
  const info = token.info ? token.info.trim() : "";
  const lang = info.split(/\s+/g)[0] || "";
  return renderCodeBlock(lang, token.content);
};

const html = computed(() => md.render(props.source || ""));

function flashCopied(btn: Element) {
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
}

function onRootClick(e: MouseEvent) {
  const el = root.value;
  if (!el) return;
  const target = e.target as HTMLElement;

  const copyBtn = target.closest("[data-code-copy]");
  if (copyBtn && el.contains(copyBtn)) {
    const codeEl = copyBtn.closest(".code")?.querySelector("pre code");
    if (!codeEl) return;
    void navigator.clipboard.writeText(codeEl.textContent ?? "").then(() => flashCopied(copyBtn));
    return;
  }

  const wrapBtn = target.closest("[data-code-wrap]");
  if (wrapBtn && el.contains(wrapBtn)) {
    const code = wrapBtn.closest(".code");
    if (!code) return;
    code.classList.toggle("is-wrapped");
    wrapBtn.setAttribute("aria-pressed", code.classList.contains("is-wrapped") ? "true" : "false");
    return;
  }

  const dlBtn = target.closest("[data-code-download]");
  if (dlBtn && el.contains(dlBtn)) {
    const code = dlBtn.closest(".code");
    if (!code) return;
    const lang = (code.getAttribute("data-lang") || "txt").toLowerCase();
    const text = code.querySelector("pre code")?.textContent ?? "";
    const ext = CODE_EXT[lang] ?? (hljs.getLanguage(lang) ? lang : "txt");
    const blob = new Blob([text], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `snippet.${ext}`;
    a.click();
    URL.revokeObjectURL(url);
  }
}
</script>

<template>
  <div ref="root" class="prose" @click="onRootClick" v-html="html" />
</template>

<style scoped>
/* Maya 的 prose 排版（components.css），scoped 以免外溢 */
.prose {
  overflow-wrap: anywhere;
  font-size: 16px;
  line-height: 1.65;
  color: rgb(var(--ink));
}
.prose :deep(> *) {
  margin: 0;
}
.prose :deep(> * + *) {
  margin-top: 12px;
}
.prose :deep(p),
.prose :deep(li) {
  line-height: inherit;
}
.prose :deep(h1),
.prose :deep(h2),
.prose :deep(h3),
.prose :deep(h4) {
  font-weight: 600;
  line-height: 1.4;
  color: rgb(var(--ink));
}
.prose :deep(h1) {
  font-size: 22px;
  margin-top: 20px;
}
.prose :deep(h2) {
  font-size: 19px;
  margin-top: 20px;
}
.prose :deep(h3) {
  font-size: 18px;
  margin-top: 20px;
}
.prose :deep(h4) {
  font-size: 16px;
  margin-top: 16px;
}
.prose :deep(ul),
.prose :deep(ol) {
  padding-left: 24px;
}
.prose :deep(ul) {
  list-style: disc;
}
.prose :deep(ol) {
  list-style: decimal;
}
.prose :deep(li + li) {
  margin-top: 6px;
}
.prose :deep(li > ul),
.prose :deep(li > ol) {
  margin-top: 6px;
}
.prose :deep(blockquote) {
  padding-left: 14px;
  border-left: 3px solid rgb(var(--line-strong));
  color: rgb(var(--ink-soft));
}
.prose :deep(hr) {
  border: 0;
  border-top: 1px solid rgb(var(--line));
  margin: 20px 0;
}
.prose :deep(a) {
  color: rgb(var(--accent-text));
}
.prose :deep(a:hover) {
  color: rgb(var(--ink));
}
.prose :deep(strong) {
  font-weight: 600;
}
.prose :deep(code) {
  font-family: ui-monospace, "SF Mono", "Cascadia Code", Consolas, "Liberation Mono", monospace;
  font-size: 0.875em;
  padding: 1px 6px;
  border-radius: 6px;
  background: rgb(var(--surface-2));
  border: 1px solid rgb(var(--line));
}
.prose :deep(pre code) {
  padding: 0;
  border: 0;
  background: transparent;
  font-size: inherit;
}
.prose :deep(table) {
  border-collapse: collapse;
  font-size: 14px;
  min-width: 50%;
  width: 100%;
}
.prose :deep(th),
.prose :deep(td) {
  padding: 8px 12px;
  border-bottom: 1px solid rgb(var(--line));
  text-align: left;
}
.prose :deep(th) {
  font-weight: 600;
  color: rgb(var(--ink));
  border-bottom-color: rgb(var(--line-strong));
}

/* 代码块：两套主题下恒暗（Maya 的 .code） */
.prose :deep(.code) {
  margin: 6px 0;
  border-radius: 12px;
  overflow: hidden;
  background: var(--code-bg);
  border: 1px solid rgb(var(--line));
}
.prose :deep(> .code) {
  margin-top: 16px;
}
.prose :deep(.code__head) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  height: 38px;
  padding: 0 6px 0 14px;
  background: var(--code-head);
  color: var(--code-dim);
  font-size: 12px;
  font-weight: 500;
}
.prose :deep(.code__actions) {
  display: flex;
  align-items: center;
  gap: 2px;
}
.prose :deep(.code__btn) {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  height: 28px;
  min-width: 28px;
  padding: 0 6px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--code-dim);
}
.prose :deep(.code__btn:hover),
.prose :deep(.code__btn[aria-pressed="true"]) {
  background: rgba(255, 255, 255, 0.08);
  color: var(--code-ink);
}
.prose :deep(.code pre) {
  margin: 0;
  padding: 14px 16px;
  overflow: auto;
  max-height: 70vh;
  font-family: ui-monospace, "SF Mono", "Cascadia Code", Consolas, "Liberation Mono", monospace;
  font-size: 13px;
  line-height: 1.6;
  color: var(--code-ink);
  tab-size: 4;
}
.prose :deep(.code.is-wrapped pre) {
  white-space: pre-wrap;
  overflow-wrap: anywhere;
}
</style>

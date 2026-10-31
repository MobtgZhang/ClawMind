/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{vue,js,ts,jsx,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        canvas: "rgb(var(--canvas) / <alpha-value>)",
        side: "rgb(var(--side) / <alpha-value>)",
        surface: {
          DEFAULT: "rgb(var(--surface) / <alpha-value>)",
          2: "rgb(var(--surface-2) / <alpha-value>)",
          3: "rgb(var(--surface-3) / <alpha-value>)",
        },
        line: {
          DEFAULT: "rgb(var(--line) / <alpha-value>)",
          strong: "rgb(var(--line-strong) / <alpha-value>)",
        },
        ink: {
          DEFAULT: "rgb(var(--ink) / <alpha-value>)",
          soft: "rgb(var(--ink-soft) / <alpha-value>)",
          muted: "rgb(var(--ink-muted) / <alpha-value>)",
        },
        accent: {
          DEFAULT: "rgb(var(--accent) / <alpha-value>)",
          hover: "rgb(var(--accent-hover) / <alpha-value>)",
          ink: "rgb(var(--accent-ink) / <alpha-value>)",
          text: "rgb(var(--accent-text) / <alpha-value>)",
          tint: "var(--accent-tint)",
        },
        info: {
          DEFAULT: "rgb(var(--info) / <alpha-value>)",
          tint: "var(--info-tint)",
          text: "rgb(var(--info-text) / <alpha-value>)",
        },
        ok: {
          DEFAULT: "rgb(var(--ok) / <alpha-value>)",
          tint: "var(--ok-tint)",
          text: "rgb(var(--ok-text) / <alpha-value>)",
        },
        warn: {
          DEFAULT: "rgb(var(--warn) / <alpha-value>)",
          tint: "var(--warn-tint)",
          text: "rgb(var(--warn-text) / <alpha-value>)",
        },
        danger: {
          DEFAULT: "rgb(var(--danger) / <alpha-value>)",
          tint: "var(--danger-tint)",
          text: "rgb(var(--danger-text) / <alpha-value>)",
        },
        code: {
          bg: "var(--code-bg)",
          head: "var(--code-head)",
          ink: "var(--code-ink)",
          dim: "var(--code-dim)",
        },
      },
      fontFamily: {
        sans: [
          "Outfit",
          "system-ui",
          "-apple-system",
          "PingFang SC",
          "Hiragino Sans GB",
          "Microsoft YaHei",
          "Segoe UI",
          "Roboto",
          "sans-serif",
        ],
        mono: [
          "ui-monospace",
          "SF Mono",
          "Cascadia Code",
          "Consolas",
          "Liberation Mono",
          "monospace",
        ],
      },
    },
  },
  plugins: [],
};

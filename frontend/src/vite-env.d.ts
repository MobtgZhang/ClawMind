/// <reference types="vite/client" />

declare module "markdown-it-texmath";

interface ImportMetaEnv {
  readonly VITE_API_BASE?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

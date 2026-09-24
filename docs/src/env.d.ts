interface ImportMetaEnv {
  readonly PUBLIC_DOCS_VERSION?: string;
  readonly PUBLIC_DOCS_COMMIT?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

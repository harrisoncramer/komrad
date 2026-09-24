export const BUILD = {
  version: import.meta.env.PUBLIC_DOCS_VERSION || "dev",
  commit: import.meta.env.PUBLIC_DOCS_COMMIT || "",
};

const base = import.meta.env.BASE_URL.replace(/\/$/, "");

export const link = (path: string) => `${base}${path}`;

export const withoutBase = (pathname: string) => pathname.slice(base.length) || "/";

import { CANONICAL_VERSION } from "./consts";

export const BUILD = {
  version: import.meta.env.PUBLIC_DOCS_VERSION || "dev",
  commit: import.meta.env.PUBLIC_DOCS_COMMIT || "",
};

const base = import.meta.env.BASE_URL.replace(/\/$/, "");

export const canonical = (pathname: string) =>
  `${import.meta.env.SITE.replace(/\/$/, "")}/${CANONICAL_VERSION}${pathname.slice(base.length) || "/"}`;

export const link = (path: string) => {
  const [route = "", hash] = path.split("#");
  const slashed = route.endsWith("/") || /\.[a-z0-9]+$/i.test(route) ? route : `${route}/`;
  return `${base}${slashed}${hash === undefined ? "" : `#${hash}`}`;
};

export const media = (name: string) =>
  `${import.meta.env.DEV ? "" : import.meta.env.SITE.replace(/\/$/, "")}/media/${name}`;

export const BUILD = {
  version: import.meta.env.PUBLIC_DOCS_VERSION || "dev",
  commit: import.meta.env.PUBLIC_DOCS_COMMIT || "",
};

const base = import.meta.env.BASE_URL.replace(/\/$/, "");

export const link = (path: string) => {
  const [route = "", hash] = path.split("#");
  const slashed = route.endsWith("/") || /\.[a-z0-9]+$/i.test(route) ? route : `${route}/`;
  return `${base}${slashed}${hash === undefined ? "" : `#${hash}`}`;
};

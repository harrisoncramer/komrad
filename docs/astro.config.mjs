import { defineConfig } from "astro/config";
import mdx from "@astrojs/mdx";
import react from "@astrojs/react";
import sitemap from "@astrojs/sitemap";
import { unified } from "@astrojs/markdown-remark";
import tailwindcss from "@tailwindcss/vite";
import { createReadStream, existsSync, statSync } from "node:fs";
import { basename, extname } from "node:path";
import { codeTheme } from "./src/code-theme.mjs";

const version = process.env.PUBLIC_DOCS_VERSION || "dev";
const base = `/${version}/`;

const pagefindDir = new URL("./dist/pagefind/", import.meta.url);
const mediaDir = new URL("../recordings/prod/", import.meta.url);
const publishedMedia = "https://komrad.dev/media";

const absoluteGuideLinks = () => (tree, file) => {
  const page = new URL(`guides/${basename(file.path, extname(file.path))}/`, `http://docs${base}`);
  const visit = (node) => {
    const href = node.properties?.href;
    if (node.tagName === "a" && typeof href === "string" && href.startsWith(".")) {
      const target = new URL(href, page);
      node.properties.href = `${target.pathname}${target.hash}`;
    }
    node.children?.forEach(visit);
  };
  visit(tree);
};

const pagefindTypes = {
  ".js": "application/javascript",
  ".json": "application/json",
  ".css": "text/css",
  ".pagefind": "application/wasm",
};

const pagefind = {
  name: "pagefind",
  resolveId(id) {
    if (id.endsWith("/pagefind/pagefind.js")) {
      return { id, external: true };
    }
  },
  configureServer(server) {
    const serve = (req, res, next) => {
      const requested = new URL("." + req.url.split("?")[0], pagefindDir);
      if (!requested.href.startsWith(pagefindDir.href)) return next();
      if (!existsSync(requested) || !statSync(requested).isFile()) return next();
      const type = pagefindTypes[extname(requested.pathname)];
      if (type) res.setHeader("Content-Type", type);
      createReadStream(requested).pipe(res);
    };
    server.middlewares.use(`${base}pagefind`, serve);
    server.middlewares.use("/pagefind", serve);
  },
};

const mediaTypes = {
  ".mp4": "video/mp4",
  ".webm": "video/webm",
  ".webp": "image/webp",
  ".png": "image/png",
};

const media = {
  name: "media",
  configureServer(server) {
    server.middlewares.use("/media", (req, res, next) => {
      const requested = new URL("." + req.url.split("?")[0], mediaDir);
      if (!requested.href.startsWith(mediaDir.href)) return next();
      if (!(extname(requested.pathname) in mediaTypes)) return next();
      if (!existsSync(requested) || !statSync(requested).isFile()) {
        res.statusCode = 302;
        res.setHeader("Location", `${publishedMedia}/${basename(requested.pathname)}`);
        res.end();
        return;
      }

      const size = statSync(requested).size;
      res.setHeader("Content-Type", mediaTypes[extname(requested.pathname)] ?? "application/octet-stream");
      res.setHeader("Accept-Ranges", "bytes");

      const range = /^bytes=(\d*)-(\d*)$/.exec(req.headers.range ?? "");
      if (!range) {
        res.setHeader("Content-Length", size);
        createReadStream(requested).pipe(res);
        return;
      }

      const start = range[1] === "" ? size - Number(range[2]) : Number(range[1]);
      const end = range[1] !== "" && range[2] !== "" ? Math.min(Number(range[2]), size - 1) : size - 1;
      res.statusCode = 206;
      res.setHeader("Content-Range", `bytes ${start}-${end}/${size}`);
      res.setHeader("Content-Length", end - start + 1);
      createReadStream(requested, { start, end }).pipe(res);
    });
  },
};

export default defineConfig({
  site: "https://komrad.dev",
  base,
  trailingSlash: "ignore",
  integrations: [mdx(), react(), sitemap({ filter: (page) => !page.endsWith("/404/") })],
  vite: {
    plugins: [tailwindcss(), pagefind, media],
  },
  markdown: {
    processor: unified({ rehypePlugins: [absoluteGuideLinks] }),
    shikiConfig: {
      theme: codeTheme,
    },
  },
});

import { existsSync } from "node:fs";
import { readFile, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { join } from "node:path";
import domino from "@mixmark-io/domino";
import TurndownService from "turndown";
import { gfm } from "turndown-plugin-gfm";

const hidden = "script, style, button, [aria-hidden='true'], [data-pagefind-ignore]";

function converter(page, twins) {
  const turndown = new TurndownService({
    headingStyle: "atx",
    codeBlockStyle: "fenced",
    bulletListMarker: "-",
    emDelimiter: "_",
  });
  turndown.use(gfm);

  turndown.addRule("code", {
    filter: "pre",
    replacement: (_, node) => {
      const language = node.getAttribute("data-language") ?? "";
      return `\n\n\`\`\`${language === "plaintext" ? "" : language}\n${node.textContent.replace(/\n$/, "")}\n\`\`\`\n\n`;
    },
  });

  turndown.addRule("video", {
    filter: "video",
    replacement: (_, node) => {
      const sources = Array.from(node.querySelectorAll("source")).map((source) => source.getAttribute("src"));
      const src = sources.find((source) => source.endsWith(".mp4")) ?? sources[0] ?? node.getAttribute("src");
      return `[Video: ${node.getAttribute("aria-label") ?? ""}](${new URL(src, page).href})`;
    },
  });

  turndown.addRule("image", {
    filter: "img",
    replacement: (_, node) => `![${node.getAttribute("alt") ?? ""}](${new URL(node.getAttribute("src"), page).href})`,
  });

  turndown.addRule("trait", {
    filter: (node) => node.getAttribute("class")?.split(" ").includes("trait"),
    replacement: (content) => `${content.trim()} · `,
  });

  turndown.addRule("cell", {
    filter: ["th", "td"],
    replacement: (content, node) => {
      const prefix = node.parentNode.firstElementChild === node ? "| " : " ";
      return `${prefix}${content
        .trim()
        .replace(/\s*\n\s*/g, " ")
        .replaceAll("|", "\\|")} |`;
    },
  });

  turndown.addRule("alert", {
    filter: (node) => node.nodeName === "ASIDE",
    replacement: (content) => `\n\n${content.trim().replace(/^/gm, "> ")}\n\n`,
  });

  turndown.addRule("link", {
    filter: (node) => node.nodeName === "A" && node.getAttribute("href"),
    replacement: (content, node) => {
      const target = new URL(node.getAttribute("href"), page);
      const twin = target.origin === page.origin && twins.get(target.pathname);
      return `[${content}](${twin ? `${twin}${target.hash}` : target.href})`;
    },
  });

  return turndown;
}

export default function markdownTwins() {
  let site;
  let base;

  return {
    name: "markdown-twins",
    hooks: {
      "astro:config:done": ({ config }) => {
        site = config.site;
        base = config.base;
      },
      "astro:build:done": async ({ dir, pages, logger }) => {
        const root = fileURLToPath(dir);
        const docs = [];

        for (const { pathname } of pages) {
          const page = join(root, pathname, "index.html");
          if (pathname === "" || !existsSync(page)) continue;
          const article = domino
            .createDocument(await readFile(page, "utf8"))
            .querySelector("article[data-pagefind-body]");
          if (!article) continue;
          const file = `${pathname.replace(/\/$/, "")}.md`;
          docs.push({ pathname, file, article });
        }

        const twins = new Map(docs.map((doc) => [`${base}${doc.pathname}`, new URL(`${base}${doc.file}`, site).href]));

        for (const { pathname, file, article } of docs) {
          Array.from(article.querySelectorAll(hidden)).forEach((node) => node.parentNode.removeChild(node));
          const markdown = converter(new URL(`${base}${pathname}`, site), twins).turndown(article);
          await writeFile(join(root, file), `${markdown.trim()}\n`);
        }

        const llms = await readFile(join(root, "llms.txt"), "utf8");
        const guides = /^## Guides\n([\s\S]*?)\n## /m.exec(llms)?.[1] ?? "";
        const full = await Promise.all(
          [...guides.matchAll(/\]\(([^)]+\.md)\)/g)].map(([, href]) =>
            readFile(join(root, new URL(href).pathname.slice(base.length)), "utf8"),
          ),
        );
        await writeFile(join(root, "llms-full.txt"), full.join("\n---\n\n"));

        logger.info(`wrote ${docs.length} markdown twins and llms-full.txt`);
      },
    },
  };
}

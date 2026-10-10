import type { APIRoute } from "astro";
import { SITE_DESCRIPTION, SITE_TITLE } from "../consts";
import { getGuides } from "../guides";
import { api, commands } from "../reference";
import { link } from "../site";

const entry = (title: string, path: string, description?: string) =>
  `- [${title}](${new URL(link(path), import.meta.env.SITE).href})${description ? `: ${description}` : ""}`;

const section = (heading: string, entries: string[]) => [`## ${heading}`, "", ...entries].join("\n");

export const GET: APIRoute = async () => {
  const guides = await getGuides();
  const body = [
    `# ${SITE_TITLE}`,
    `> ${SITE_DESCRIPTION}`,
    "Each .md link below is the Markdown form of a page on this site, whose HTML sits at the same address without the .md.",
    section(
      "Guides",
      guides.map((guide) => entry(guide.data.title, `/guides/${guide.id}.md`, guide.data.description)),
    ),
    section(
      "CLI",
      commands.map((command) =>
        entry(command.path, command.slug === "" ? "/cli.md" : `/cli/${command.slug}.md`, command.short),
      ),
    ),
    section(
      "API",
      api.services.flatMap((service) =>
        service.methods.map((method) => entry(method.tool, `/api/${service.slug}/${method.slug}.md`, method.summary)),
      ),
    ),
    section("Optional", [
      entry("Configuration", "/config.md", "Every setting in config.toml, with its type and default."),
      entry("Full guides", "/llms-full.txt", "Every guide in one file."),
    ]),
  ].join("\n\n");
  return new Response(`${body}\n`, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
};

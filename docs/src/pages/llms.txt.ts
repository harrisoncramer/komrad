import type { APIRoute } from "astro";
import { SITE_DESCRIPTION, SITE_TITLE } from "../consts";
import { getGuides } from "../guides";
import { absolute, commandMarkdownPath, guideMarkdownPath, methodMarkdownPath } from "../markdown";
import { api, commands } from "../reference";

const entry = (title: string, path: string, description?: string) =>
  `- [${title}](${absolute(path)})${description ? `: ${description}` : ""}`;

export const GET: APIRoute = async () => {
  const guides = await getGuides();
  const body = [
    `# ${SITE_TITLE}`,
    `> ${SITE_DESCRIPTION}`,
    "Every page below is the Markdown form of a page on this site. The HTML page sits at the same address without the .md.",
    [
      "## Guides\n",
      ...guides.map((guide) => entry(guide.data.title, guideMarkdownPath(guide.id), guide.data.description)),
    ].join("\n"),
    [
      "## CLI\n",
      ...commands.map((command) => entry(command.path, commandMarkdownPath(command.slug), command.short)),
    ].join("\n"),
    [
      "## API\n",
      ...api.services.flatMap((service) =>
        service.methods.map((method) => entry(method.tool, methodMarkdownPath(service, method), method.summary)),
      ),
    ].join("\n"),
    [
      "## Optional\n",
      entry("Configuration", "/config", "Every setting in config.toml, with its type and default."),
      entry("Full guides", "/llms-full.txt", "Every guide in one file."),
    ].join("\n"),
  ].join("\n\n");
  return new Response(`${body}\n`, { headers: { "Content-Type": "text/plain; charset=utf-8" } });
};

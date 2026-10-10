import type { APIRoute, GetStaticPaths } from "astro";
import { commandMarkdown } from "../../markdown";
import { commands } from "../../reference";
import type { Command } from "../../reference/types";

export const getStaticPaths = (() =>
  commands
    .filter((command) => command.slug !== "")
    .map((command) => ({ params: { slug: command.slug }, props: { command } }))) satisfies GetStaticPaths;

export const GET: APIRoute<{ command: Command }> = ({ props }) =>
  new Response(commandMarkdown(props.command), { headers: { "Content-Type": "text/markdown; charset=utf-8" } });

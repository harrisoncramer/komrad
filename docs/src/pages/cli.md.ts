import type { APIRoute } from "astro";
import { commandMarkdown } from "../markdown";
import { commandBySlug } from "../reference";

export const GET: APIRoute = () => {
  const root = commandBySlug.get("");
  if (!root) throw new Error("cli.json has no root command");
  return new Response(commandMarkdown(root), { headers: { "Content-Type": "text/markdown; charset=utf-8" } });
};

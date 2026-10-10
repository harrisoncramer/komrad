import type { APIRoute } from "astro";
import { getGuides } from "../guides";
import { guideMarkdown } from "../markdown";

export const GET: APIRoute = async () => {
  const guides = await getGuides();
  return new Response(guides.map(guideMarkdown).join("\n---\n\n"), {
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
};

import type { APIRoute, GetStaticPaths } from "astro";
import type { CollectionEntry } from "astro:content";
import { getGuides } from "../../guides";
import { guideMarkdown } from "../../markdown";

export const getStaticPaths = (async () => {
  const guides = await getGuides();
  return guides.map((guide) => ({ params: { slug: guide.id }, props: { guide } }));
}) satisfies GetStaticPaths;

export const GET: APIRoute<{ guide: CollectionEntry<"guides"> }> = ({ props }) =>
  new Response(guideMarkdown(props.guide), { headers: { "Content-Type": "text/markdown; charset=utf-8" } });

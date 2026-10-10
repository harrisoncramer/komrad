import { getCollection } from "astro:content";

export const GUIDE_GROUPS = [
  { id: "start", title: "Getting started" },
  { id: "operations", title: "Operations" },
  { id: "architecture", title: "Architecture" },
] as const;

export const getGuides = async () => {
  const guides = await getCollection("guides");
  return guides.sort(
    (a, b) => (a.data.order ?? Infinity) - (b.data.order ?? Infinity) || a.data.title.localeCompare(b.data.title),
  );
};

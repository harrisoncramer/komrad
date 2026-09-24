import { getCollection } from "astro:content";

export const getGuides = async () => {
  const guides = await getCollection("guides");
  return guides.sort(
    (a, b) => (a.data.order ?? Infinity) - (b.data.order ?? Infinity) || a.data.title.localeCompare(b.data.title),
  );
};

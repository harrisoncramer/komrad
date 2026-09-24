import { getCollection } from "astro:content";
import type { SectionId } from "./consts";

export const getPages = async (section: SectionId) => {
  const pages = await getCollection(section);
  return pages.sort(
    (a, b) =>
      (a.data.order ?? Infinity) - (b.data.order ?? Infinity) ||
      a.data.title.localeCompare(b.data.title),
  );
};

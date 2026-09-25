import { defineCollection } from "astro:content";
import { glob } from "astro/loaders";
import { z } from "astro/zod";

const guides = defineCollection({
  loader: glob({ pattern: "**/*.{md,mdx}", base: "./src/content/guides" }),
  schema: z.object({
    title: z.string(),
    description: z.string().optional(),
    group: z.enum(["start", "operations", "architecture"]),
    order: z.number().optional(),
  }),
});

export const collections = { guides };

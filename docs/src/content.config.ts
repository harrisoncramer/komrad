import { defineCollection } from "astro:content";
import { glob } from "astro/loaders";
import { z } from "astro/zod";

const page = z.object({
  title: z.string(),
  description: z.string().optional(),
  order: z.number().optional(),
});

const guides = defineCollection({
  loader: glob({ pattern: "**/*.{md,mdx}", base: "./src/content/guides" }),
  schema: page,
});

const api = defineCollection({
  loader: glob({ pattern: "**/*.{md,mdx}", base: "./src/content/api" }),
  schema: page,
});

export const collections = { guides, api };

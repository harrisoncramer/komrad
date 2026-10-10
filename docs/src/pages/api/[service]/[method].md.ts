import type { APIRoute, GetStaticPaths } from "astro";
import { methodMarkdown } from "../../../markdown";
import { api } from "../../../reference";
import type { Method, Service } from "../../../reference/types";

export const getStaticPaths = (() =>
  api.services.flatMap((service) =>
    service.methods.map((method) => ({
      params: { service: service.slug, method: method.slug },
      props: { service, method },
    })),
  )) satisfies GetStaticPaths;

export const GET: APIRoute<{ service: Service; method: Method }> = ({ props }) =>
  new Response(methodMarkdown(props.service, props.method), {
    headers: { "Content-Type": "text/markdown; charset=utf-8" },
  });

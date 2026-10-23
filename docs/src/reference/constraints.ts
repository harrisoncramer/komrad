import type { Constraints } from "./types";

const atLeast = (count: number | undefined, unit: string, required: boolean) => {
  if (count === undefined) return [];
  if (count > 1) return [`at least ${String(count)} ${unit}`];
  return count === 1 && !required ? ["not empty"] : [];
};

const atMost = (count: number | undefined, unit: string) =>
  count === undefined ? [] : [`at most ${String(count)} ${unit}`];

const bounds = ({ minimum, exclusiveMinimum, maximum, exclusiveMaximum }: Constraints) => {
  if (minimum !== undefined && maximum !== undefined) return [`between ${String(minimum)} and ${String(maximum)}`];
  return [
    minimum !== undefined && `at least ${String(minimum)}`,
    exclusiveMinimum !== undefined && `more than ${String(exclusiveMinimum)}`,
    maximum !== undefined && `at most ${String(maximum)}`,
    exclusiveMaximum !== undefined && `less than ${String(exclusiveMaximum)}`,
  ].filter((part) => part !== false);
};

export const describeConstraints = (constraints: Constraints, required: boolean): string[] => [
  ...atLeast(constraints.minLength, "characters", required),
  ...atMost(constraints.maxLength, "characters"),
  ...atMost(constraints.maxBytes, "bytes"),
  ...(constraints.pattern ? [`matching ${constraints.pattern}`] : []),
  ...(constraints.enum ? ["one of", ...constraints.enum] : []),
  ...bounds(constraints),
  ...atLeast(constraints.minItems, "items", required),
  ...atMost(constraints.maxItems, "items"),
  ...(constraints.uniqueItems ? ["no repeats"] : []),
  ...(constraints.items ? describeConstraints(constraints.items, false).map((part) => `each ${part}`) : []),
  ...(constraints.rules ?? []),
];

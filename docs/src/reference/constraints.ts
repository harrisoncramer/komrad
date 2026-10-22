import type { Constraints } from "./types";

const atLeast = (count: number | undefined, unit: string, required: boolean) => {
  if (count === undefined) return [];
  if (count > 1) return [`at least ${String(count)} ${unit}`];
  return count === 1 && !required ? ["not empty"] : [];
};

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
  ...(constraints.maxLength === undefined ? [] : [`at most ${String(constraints.maxLength)} characters`]),
  ...(constraints.pattern ? [`matching ${constraints.pattern}`] : []),
  ...bounds(constraints),
  ...atLeast(constraints.minItems, "items", required),
  ...(constraints.maxItems === undefined ? [] : [`at most ${String(constraints.maxItems)} items`]),
  ...(constraints.uniqueItems ? ["no repeats"] : []),
  ...(constraints.items ? describeConstraints(constraints.items, false).map((part) => `each ${part}`) : []),
  ...(constraints.rules ?? []),
];

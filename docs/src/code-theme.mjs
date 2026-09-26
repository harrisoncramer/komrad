const palette = {
  background: "#080d1a",
  foreground: "#c9d4e5",
  dim: "#7a879e",
  red: "#ff8a8a",
  green: "#8fd4a8",
  yellow: "#f5c26b",
  blue: "#9ec4ff",
  magenta: "#c7a6ff",
  cyan: "#7fd6e0",
};

const rule = (scope, foreground, fontStyle) => ({
  scope,
  settings: fontStyle ? { foreground, fontStyle } : { foreground },
});

export const codeTheme = {
  name: "komrad",
  type: "dark",
  colors: {
    "editor.background": palette.background,
    "editor.foreground": palette.foreground,
  },
  tokenColors: [
    rule(["source", "text"], palette.foreground),
    rule(["comment", "punctuation.definition.comment"], palette.dim, "italic"),
    rule(["string", "string.quoted", "string.template", "markup.inline.raw"], palette.green),
    rule(["constant.numeric", "constant.language", "constant.character", "support.constant"], palette.yellow),
    rule(["keyword", "storage", "storage.type", "keyword.control", "keyword.operator.logical"], palette.magenta),
    rule(["entity.name.function", "support.function", "meta.function-call"], palette.blue),
    rule(["entity.name.type", "entity.name.class", "support.type", "support.class"], palette.cyan),
    rule(
      ["variable.parameter", "variable.other.readwrite", "entity.name.tag", "entity.other.attribute-name"],
      palette.red,
    ),
    rule(["punctuation", "keyword.operator", "meta.brace"], palette.dim),
    rule(["markup.heading", "entity.name.section"], palette.blue),
  ],
};

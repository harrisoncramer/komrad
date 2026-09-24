import js from "@eslint/js";
import astro from "eslint-plugin-astro";
import importX from "eslint-plugin-import-x";
import jsxA11y from "eslint-plugin-jsx-a11y";
import react from "eslint-plugin-react";
import reactHooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: ["dist/", "site/", ".astro/", "node_modules/", "src/data/"],
  },
  js.configs.recommended,
  ...tseslint.configs.strictTypeChecked,
  ...tseslint.configs.stylisticTypeChecked,
  ...astro.configs["flat/recommended"],
  ...astro.configs["flat/jsx-a11y-strict"],
  {
    languageOptions: {
      globals: { ...globals.browser, ...globals.node },
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
        extraFileExtensions: [".astro"],
      },
    },
    plugins: { "import-x": importX },
    rules: {
      eqeqeq: ["error", "smart"],
      "no-else-return": "error",
      "@typescript-eslint/consistent-type-definitions": ["error", "type"],
      "@typescript-eslint/consistent-type-imports": "error",
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_", varsIgnorePattern: "^_" }],
      "@typescript-eslint/switch-exhaustiveness-check": "error",
      "@typescript-eslint/prefer-nullish-coalescing": ["error", { ignorePrimitives: { string: true } }],
      "import-x/no-duplicates": "error",
      "import-x/no-self-import": "error",
    },
  },
  {
    files: ["**/*.tsx"],
    plugins: { react, "react-hooks": reactHooks, "jsx-a11y": jsxA11y },
    settings: { react: { version: "detect" } },
    rules: {
      ...react.configs.flat.recommended.rules,
      ...react.configs.flat["jsx-runtime"].rules,
      ...reactHooks.configs["recommended-latest"].rules,
      ...jsxA11y.flatConfigs.strict.rules,
      "react/prop-types": "off",
    },
  },
  {
    files: ["**/*.d.ts"],
    rules: { "@typescript-eslint/consistent-type-definitions": "off" },
  },
  {
    files: ["**/*.mjs", "**/*.astro", "**/*.astro/*.ts"],
    ...tseslint.configs.disableTypeChecked,
  },
);

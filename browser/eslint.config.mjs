import js from "@eslint/js";
import globals from "globals";

export default [
  { ignores: ["node_modules/**", "test-results/**", "playwright-report/**"] },
  js.configs.recommended,
  {
    files: ["**/*.mjs", "**/*.js"],
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: { ...globals.browser, ...globals.node },
    },
    rules: {
      // Dropping one key through a rest sibling is the intentional shape (`const { DROP: _, ...rest } = env`).
      "no-unused-vars": ["error", { ignoreRestSiblings: true }],
    },
  },
];

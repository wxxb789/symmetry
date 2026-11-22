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
      "no-unused-vars": ["error", { ignoreRestSiblings: true }],
    },
  },
];

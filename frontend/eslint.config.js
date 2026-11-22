import js from "@eslint/js";
import hooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist/**", "node_modules/**", "coverage/**"] },
  js.configs.recommended,
  ...tseslint.configs.strictTypeChecked.map((config) => ({
    ...config,
    files: ["**/*.ts", "**/*.tsx"],
  })),
  ...tseslint.configs.stylisticTypeChecked.map((config) => ({
    ...config,
    files: ["**/*.ts", "**/*.tsx"],
  })),
  {
    files: ["**/*.ts", "**/*.tsx"],
    languageOptions: {
      globals: { ...globals.browser, ...globals.node },
      parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname },
    },
    plugins: { "react-hooks": hooks },
    rules: {
      ...hooks.configs["recommended-latest"].rules,
    },
  },
  {
    // Residual rule warnings are the recorded ratchet entry: their total (229) is the
    // `--max-warnings` ceiling in `package.json`; burn-down order is program plan §10.3.
    files: ["**/*.ts", "**/*.tsx"],
    rules: {
      "@typescript-eslint/no-unnecessary-condition": "warn", // 78
      "@typescript-eslint/restrict-template-expressions": "warn", // 40
      "@typescript-eslint/no-invalid-void-type": "warn", // 26
      "max-lines-per-function": ["warn", { max: 150, skipBlankLines: true, skipComments: true }], // 16
      complexity: ["warn", 25], // 14
      "@typescript-eslint/no-deprecated": "warn", // 13
      "@typescript-eslint/no-non-null-assertion": "warn", // 11
      "@typescript-eslint/prefer-nullish-coalescing": "warn", // 8
      "@typescript-eslint/no-dynamic-delete": "warn", // 6
      "@typescript-eslint/prefer-find": "warn", // 5
      "@typescript-eslint/no-empty-function": "warn", // 4
      "@typescript-eslint/prefer-optional-chain": "warn", // 4
      "@typescript-eslint/dot-notation": "warn", // 2
      "@typescript-eslint/no-unnecessary-type-conversion": "warn", // 2
    },
  },
  { files: ["**/*.js", "**/*.mjs"], languageOptions: { globals: globals.node } },
);

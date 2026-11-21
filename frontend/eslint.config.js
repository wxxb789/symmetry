import js from "@eslint/js";
import hooks from "eslint-plugin-react-hooks";
import globals from "globals";
import tseslint from "typescript-eslint";

// Strict static-quality baseline (rf-w1-10-frontend-eslint, 2026-11-21):
// `strictTypeChecked` + `stylisticTypeChecked` + the react-hooks
// `recommended-latest` preset (which adds exactly `react-hooks/void-use-memo`
// over the `recommended` preset used before).
//
// The 515 mechanical findings these configs reported on the pre-change tree were
// applied with one `eslint --fix` pass; the strict configs reported 743 errors
// before and 229 after (42 files, `.ts`/`.tsx`), and every rule that still
// reports is downgraded to `warn` in the residual block below, each with its
// measured count. `pnpm lint` runs `eslint . --max-warnings 229`, so any NEW
// finding of ANY rule — including the two size caps and the design-deferred
// `no-unnecessary-condition` — fails the gate; the counts below sum to that
// ceiling and a burn-down slice lowers both together. No rule is set to `off`.
//
// The two size caps are counted in the ratchet rather than exempted per file:
// measured, an allowlist would need 21 file-level exemptions (14 production
// files + 7 test files; two files carry both rules) for 30 cap findings whose
// enforcement ESLint cannot shrink, while the `--max-warnings` ceiling already
// fails any new oversized function everywhere.
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
    // Residual findings after the 2026-11-21 autofix pass, measured with
    // `eslint . --format json` and summed per rule; the numbers are the recorded
    // ratchet entry, and their total (229) is the `--max-warnings` ceiling in
    // `package.json`. Burn-down order lives in the refactor program plan §10.3;
    // `no-unnecessary-condition` waits for the FE F4/F11 slices (contract
    // clause 4). The size caps (30 findings today) keep the §10.3 limits and are
    // burned down by the slices that split the named files.
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
      // Both remaining dot-notation findings sit in `frontend/vite.config.ts`
      // (`process.env["NAME"]` access), which this slice does not own; the
      // file's next owner fixes them and removes this warning.
      "@typescript-eslint/dot-notation": "warn", // 2
      "@typescript-eslint/no-unnecessary-type-conversion": "warn", // 2
    },
  },
  { files: ["**/*.js", "**/*.mjs"], languageOptions: { globals: globals.node } },
);

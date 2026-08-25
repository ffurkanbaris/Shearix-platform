import js from "@eslint/js";
import tseslint from "typescript-eslint";
export default tseslint.config({ ignores: [".next/**", "next-env.d.ts"] }, js.configs.recommended, ...tseslint.configs.recommended, { files: ["**/*.{ts,tsx}"], languageOptions: { parserOptions: { ecmaFeatures: { jsx: true } } }, rules: { "@typescript-eslint/no-explicit-any": "off" } });

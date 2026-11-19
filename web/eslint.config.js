import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

export default tseslint.config(
	{ ignores: ["dist", "src/api/schema.d.ts", "e2e/.state", "e2e/.results", "e2e/.report", "node_modules"] },
	js.configs.recommended,
	...tseslint.configs.recommended,
	reactHooks.configs.flat.recommended,
	{
		languageOptions: { globals: { ...globals.browser, ...globals.node } },
		rules: {
			// Exhaustive deps stay errors. A disable comment needs a reason.
			"@typescript-eslint/ban-ts-comment": ["error", { "ts-ignore": true, "ts-expect-error": "allow-with-description" }],
			"@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_", varsIgnorePattern: "^_" }],
			"max-lines": ["warn", { max: 300, skipBlankLines: true, skipComments: true }],
		},
	},
	{
		// Query keys live in one place so a typo is a compile error, not a cache that never refreshes.
		files: ["src/**/*.{ts,tsx}"],
		ignores: ["src/api/queries.ts", "src/queryClient.ts", "src/**/*.test.{ts,tsx}", "src/test/**"],
		rules: {
			"no-restricted-syntax": [
				"error",
				{
					selector: "Property[key.name='queryKey'][value.type='ArrayExpression']",
					message: "Use a query from api/queries.ts (or keys.* with invalidates()) instead of writing a queryKey by hand.",
				},
				{
					selector: "CallExpression[callee.property.name='invalidateQueries']",
					message: "Declare what a mutation refreshes with `meta: invalidates(keys.x)`; the query client applies it.",
				},
			],
		},
	},
	{
		files: ["e2e/**/*.ts", "playwright.config.ts", "vite.config.ts", "eslint.config.js"],
		rules: { "react-hooks/rules-of-hooks": "off" },
	},
);

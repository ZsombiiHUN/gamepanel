import js from "@eslint/js";
import globals from "globals";
import jsxA11y from "eslint-plugin-jsx-a11y";
import reactHooks from "eslint-plugin-react-hooks";
import tseslint from "typescript-eslint";

export default tseslint.config(
	{ ignores: ["dist", "src/api/schema.d.ts", "e2e/.state", "e2e/.results", "e2e/.report", "node_modules"] },
	js.configs.recommended,
	...tseslint.configs.strictTypeChecked,
	...tseslint.configs.stylisticTypeChecked,
	reactHooks.configs.flat.recommended,
	jsxA11y.flatConfigs.recommended,
	{
		languageOptions: {
			globals: { ...globals.browser, ...globals.node },
			parserOptions: {
				// playwright.config.ts belongs to e2e/tsconfig.json, which the nearest-tsconfig lookup does not find.
				projectService: { allowDefaultProject: ["playwright.config.ts"] },
				tsconfigRootDir: import.meta.dirname,
			},
		},
		linterOptions: { reportUnusedDisableDirectives: "error" },
		rules: {
			// Exhaustive deps stay errors. A disable comment needs a reason.
			"@typescript-eslint/ban-ts-comment": ["error", { "ts-ignore": true, "ts-expect-error": "allow-with-description" }],
			"@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_", varsIgnorePattern: "^_" }],
			// Numbers and booleans read fine in template strings; objects and undefined do not.
			"@typescript-eslint/restrict-template-expressions": ["error", { allowNumber: true, allowBoolean: true }],
			// `onClick={() => setOpen(false)}` is the normal React idiom.
			"@typescript-eslint/no-confusing-void-expression": ["error", { ignoreArrowShorthand: true }],
			// TanStack Router signals redirects by throwing them.
			"@typescript-eslint/only-throw-error": [
				"error",
				{ allow: [{ from: "package", package: "@tanstack/router-core", name: ["Redirect", "NotFoundError"] }] },
			],
			// Dialogs and single-purpose forms move focus to their first field on open, as the WAI-ARIA dialog pattern asks.
			"jsx-a11y/no-autofocus": "off",
			// Scrollable regions must be reachable by keyboard (axe: scrollable-region-focusable).
			"jsx-a11y/no-noninteractive-tabindex": ["error", { tags: [], roles: ["tabpanel", "region"] }],
			// Checkbox labels nest their text a few spans deep.
			"jsx-a11y/label-has-associated-control": ["error", { depth: 3 }],
			// `a || b` on strings and booleans means "empty or false falls back", which `??` would not do.
			"@typescript-eslint/prefer-nullish-coalescing": ["error", { ignorePrimitives: { string: true, boolean: true } }],
			"max-lines": ["error", { max: 500, skipBlankLines: true, skipComments: true }],
		},
	},
	{
		// Tests may assert that a value exists with `!` (a wrong guess fails the test) and stub callbacks with no-ops.
		files: ["src/**/*.test.{ts,tsx}", "src/test/**", "e2e/**/*.ts"],
		rules: {
			"@typescript-eslint/no-non-null-assertion": "off",
			"@typescript-eslint/no-empty-function": "off",
			// Fakes stand in for async browser and network APIs without awaiting anything themselves.
			"@typescript-eslint/require-await": "off",
		},
	},
	{
		// Polyfills for what jsdom lacks, which the DOM types say is always there.
		files: ["src/test/setup.ts"],
		rules: {
			"@typescript-eslint/no-unnecessary-condition": "off",
			"@typescript-eslint/unbound-method": "off",
		},
	},
	{
		// Plain JS config files are not part of a TypeScript project.
		files: ["**/*.js"],
		...tseslint.configs.disableTypeChecked,
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
	// Plain Node helper scripts: no tsconfig, so lint them without type information.
	{ files: ["scripts/**/*.mjs"], ...tseslint.configs.disableTypeChecked },
);

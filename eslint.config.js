import js from '@eslint/js';
import globals from 'globals';
export default [
  { ignores: ['node_modules/**', 'output/**'] },
  js.configs.recommended,
  {
    files: ['web/**/*.js'],
    languageOptions: { ecmaVersion: 2022, sourceType: 'module', globals: globals.browser },
    rules: { 'no-shadow': 'error' },
  },
  {
    files: ['tests/**/*.{js,cjs}'],
    languageOptions: { globals: { ...globals.node, ...globals.browser } },
  },
];

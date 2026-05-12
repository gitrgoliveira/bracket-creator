import js from "@eslint/js";
import reactPlugin from "eslint-plugin-react";
import globals from "globals";

export default [
  {
    ignores: ["node_modules/**", "dist/**", "vendor/**"],
  },
  js.configs.recommended,
  {
    files: ["js/**/*.{js,jsx}", "*.{js,jsx}"],
    plugins: {
      react: reactPlugin,
    },
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: {
        ...globals.browser,
        ...globals.node,
        window: "readonly",
        document: "readonly",
        navigator: "readonly",
        global: "readonly",
        React: "readonly",
        ReactDOM: "readonly",
        preact: "readonly",
        vi: "readonly",
      },
      parserOptions: {
        ecmaFeatures: {
          jsx: true,
        },
      },
    },
    settings: {
      react: {
        version: "18.0.0",
      },
    },
    rules: {
      ...reactPlugin.configs.recommended.rules,
      "react/react-in-jsx-scope": "off",
      "react/prop-types": "off",
      "no-unused-vars": ["warn", { 
        "argsIgnorePattern": "^_", 
        "varsIgnorePattern": "^_",
        "caughtErrorsIgnorePattern": "^_"
      }],
      "no-undef": "error",
      "no-useless-assignment": "off",
      "react/jsx-no-undef": "off",
      "react/no-unescaped-entities": "off",
    },
  },
];

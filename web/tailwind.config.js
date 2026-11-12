/** @type {import('tailwindcss').Config} */
export default {
  darkMode: 'class',
  content: [
    "./index.html",
    "./src/**/*.{js,ts,jsx,tsx}",
  ],
  theme: {
    extend: {
      fontFamily: {
        sans: ['Inter', '-apple-system', 'BlinkMacSystemFont', 'Segoe UI', 'Roboto', 'sans-serif'],
        serif: ['Newsreader', 'Georgia', 'Cambria', 'Times', 'serif'],
        mono: ['"JetBrains Mono"', 'Menlo', 'ui-monospace', 'SFMono-Regular', 'monospace'],
      },
      colors: {
        dim: {
          light: '#666666',
          dark: '#888888',
        }
      },
      letterSpacing: {
        tighter: '-0.04em',
        tight: '-0.02em',
        normal: '0',
        wide: '0.02em',
        wider: '0.05em',
      }
    },
  },
  plugins: [],
}

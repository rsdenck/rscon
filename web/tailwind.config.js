/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  theme: {
    extend: {
      colors: {
        ink: '#0b1220',
        panel: '#111a2b',
        edge: '#24344f',
        accent: '#3dd6c4',
      },
    },
  },
  plugins: [],
}

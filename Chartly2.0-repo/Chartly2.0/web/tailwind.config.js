/** @type {import("tailwindcss").Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      // Keep extensions minimal; consumers can add design tokens later.
    },
  },
  plugins: [],
};

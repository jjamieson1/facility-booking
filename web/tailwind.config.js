/** @type {import('tailwindcss').Config} */
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // Rivermont brand: a calm municipal blue-teal.
        brand: {
          50: "#eef7f9", 100: "#d6ecf0", 500: "#2a7f8e", 600: "#236b78", 700: "#1d5763",
        },
      },
    },
  },
  plugins: [],
};

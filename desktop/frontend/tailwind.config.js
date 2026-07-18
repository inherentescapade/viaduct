/** @type {import('tailwindcss').Config} */

import animate from "tailwindcss-animate";

const withAlpha = (v) => `hsl(var(${v}) / <alpha-value>)`;

export default {
  darkMode: "class",
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  theme: {
    extend: {
      colors: {
        // shadcn semantic tokens
        background: withAlpha("--background"),
        foreground: withAlpha("--foreground"),
        card: { DEFAULT: withAlpha("--card"), foreground: withAlpha("--card-foreground") },
        popover: { DEFAULT: withAlpha("--popover"), foreground: withAlpha("--popover-foreground") },
        primary: { DEFAULT: withAlpha("--primary"), foreground: withAlpha("--primary-foreground") },
        secondary: { DEFAULT: withAlpha("--secondary"), foreground: withAlpha("--secondary-foreground") },
        muted: { DEFAULT: withAlpha("--muted"), foreground: withAlpha("--muted-foreground") },
        accent: {
          // `accent`/`accent-strong`/`accent-soft` map onto the monochrome
          // primary, so accented elements render white.
          DEFAULT: withAlpha("--primary"),
          strong: withAlpha("--primary"),
          soft: withAlpha("--muted"),
          foreground: withAlpha("--primary-foreground"),
        },
        destructive: { DEFAULT: withAlpha("--destructive"), foreground: withAlpha("--destructive-foreground") },
        border: withAlpha("--border"),
        input: withAlpha("--input"),
        ring: withAlpha("--ring"),

        // Shorthand aliases used throughout the components.
        canvas: withAlpha("--background"),
        plate: withAlpha("--card"),
        ink: withAlpha("--foreground"),
        dim: withAlpha("--muted-foreground"),
        line: withAlpha("--border"),
        danger: {
          DEFAULT: withAlpha("--destructive"),
          strong: withAlpha("--destructive-strong"),
          soft: withAlpha("--destructive-soft"),
        },
        warn: {
          DEFAULT: withAlpha("--warning"),
          soft: withAlpha("--warning-soft"),
        },
      },
      borderRadius: {
        lg: "var(--radius)",
        xl: "calc(var(--radius) + 0.25rem)",
        "2xl": "calc(var(--radius) + 0.5rem)",
        "3xl": "calc(var(--radius) + 0.75rem)",
      },
      boxShadow: {
        soft: "0 10px 34px rgba(0,0,0,0.55)",
        // The frosted-panel shadow: a top inset highlight, a tight contact
        // shadow, and a soft long drop so cards lift off the canvas.
        card: "inset 0 1px 0 rgba(255,255,255,0.06), 0 1px 2px rgba(0,0,0,0.5), 0 24px 48px -24px rgba(0,0,0,0.8)",
        // A restrained white "glow" for highlighted elements.
        glow: "0 8px 30px rgba(255,255,255,0.08), inset 0 1px 0 rgba(255,255,255,0.18)",
      },
      backdropBlur: {
        glass: "16px",
      },
      fontFamily: {
        sans: ["Inter", "ui-rounded", "-apple-system", "system-ui", "sans-serif"],
        mono: ["ui-monospace", "SFMono-Regular", "Menlo", "monospace"],
      },
      keyframes: {
        "fade-up": {
          "0%": { opacity: "0", transform: "translateY(8px)" },
          "100%": { opacity: "1", transform: "translateY(0)" },
        },
        shimmer: {
          "100%": { transform: "translateX(100%)" },
        },
        // A ~1/3-width bar sweeping fully across its track (it must translate by
        // ~3x its own width to clear both edges) — used for indeterminate
        // "preparing" progress.
        indeterminate: {
          "0%": { transform: "translateX(-120%)" },
          "100%": { transform: "translateX(340%)" },
        },
        "rain-in": {
          "0%": { opacity: "0", transform: "translateY(-12px) scale(0.98)" },
          "100%": { opacity: "1", transform: "translateY(0) scale(1)" },
        },
        "grow-x": {
          "0%": { transform: "scaleX(0)" },
          "100%": { transform: "scaleX(1)" },
        },
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
      },
      animation: {
        "fade-up": "fade-up 0.35s cubic-bezier(0.22,1,0.36,1) both",
        shimmer: "shimmer 1.4s infinite",
        indeterminate: "indeterminate 1.15s ease-in-out infinite",
        "rain-in": "rain-in 0.4s cubic-bezier(0.22,1,0.36,1)",
        "grow-x": "grow-x 0.6s cubic-bezier(0.22,1,0.36,1) both",
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up": "accordion-up 0.2s ease-out",
      },
    },
  },
  plugins: [animate],
};

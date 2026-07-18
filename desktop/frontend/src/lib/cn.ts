// Re-export the shadcn cn helper so the many existing `../lib/cn` imports get
// proper Tailwind-conflict resolution (clsx + tailwind-merge) for free.
export { cn } from "./utils";

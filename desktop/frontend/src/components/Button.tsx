import type { ButtonHTMLAttributes } from "react";
import { Button as UIButton } from "./ui/button";

// Thin adapter that keeps Viaduct's existing Button API (primary/ghost/danger/
// subtle + md/lg) while delegating to the shadcn/ui Button underneath, so every
// call site renders the real accessible component.

type Variant = "primary" | "ghost" | "danger" | "subtle";
type Size = "md" | "lg";

const variantMap = {
  primary: "default",
  ghost: "ghost",
  danger: "destructive",
  subtle: "secondary",
} as const;

const sizeMap = {
  md: "default",
  lg: "lg",
} as const;

interface Props extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: Variant;
  size?: Size;
}

export function Button({ variant = "primary", size = "md", ...rest }: Props) {
  return <UIButton variant={variantMap[variant]} size={sizeMap[size]} {...rest} />;
}

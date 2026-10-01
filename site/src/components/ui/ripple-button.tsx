"use client";

import { forwardRef, useRef, useState, type ButtonHTMLAttributes, type CSSProperties, type MouseEvent } from "react";
import { useReducedMotion } from "motion/react";
import { cn } from "@/lib/utils";

interface RippleButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  rippleColor?: string;
  duration?: string;
}

export const RippleButton = forwardRef<HTMLButtonElement, RippleButtonProps>(
  ({ className, children, rippleColor = "#ffffff", duration = "600ms", onClick, ...props }, ref) => {
    const reducedMotion = useReducedMotion();
    const rippleId = useRef(0);
    const [ripples, setRipples] = useState<Array<{ x: number; y: number; size: number; key: number }>>([]);

    function handleClick(event: MouseEvent<HTMLButtonElement>) {
      if (!reducedMotion) {
        const rect = event.currentTarget.getBoundingClientRect();
        const size = Math.max(rect.width, rect.height);
        const x = (event.detail === 0 ? rect.width / 2 : event.clientX - rect.left) - size / 2;
        const y = (event.detail === 0 ? rect.height / 2 : event.clientY - rect.top) - size / 2;
        setRipples((current) => [...current, { x, y, size, key: ++rippleId.current }]);
      }
      onClick?.(event);
    }

    return (
      <button ref={ref} onClick={handleClick} {...props}
        className={cn("relative flex cursor-pointer items-center justify-center overflow-hidden rounded-lg border-2 bg-background px-4 py-2 text-center text-primary", className)}>
        <span className="contents">{children}</span>
        <span aria-hidden="true" className="pointer-events-none absolute inset-0">
          {ripples.map((ripple) => (
            <span key={ripple.key} className="animate-rippling absolute rounded-full opacity-30"
              onAnimationEnd={() => setRipples((current) => current.filter((item) => item.key !== ripple.key))}
              style={{ width: ripple.size, height: ripple.size, top: ripple.y, left: ripple.x,
                backgroundColor: rippleColor, transform: "scale(0)", "--duration": duration } as CSSProperties} />
          ))}
        </span>
      </button>
    );
  },
);
RippleButton.displayName = "RippleButton";

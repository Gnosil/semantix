import { useId } from "react";
import review from "../assets/permissions/review.png";
import delegate from "../assets/permissions/delegate.png";
import bypass from "../assets/permissions/bypass.png";
import type { ToolApprovalMode } from "../lib/types";

const sources = { ask: review, auto: delegate, yolo: bypass };

export function SemantixPermissionArt({ mode, size = 22 }: { mode: ToolApprovalMode; size?: number }) {
  const maskId = `permission-art-${useId()}`;
  return <svg width={size} height={size} viewBox="180 180 920 920" fill="none"
    aria-hidden="true" data-semantix-permission-art={mode}>
    <defs><mask id={maskId} x="0" y="0" width="1254" height="1254"
      maskUnits="userSpaceOnUse" style={{ maskType: "alpha" }}>
      <image href={sources[mode]} width="1254" height="1254" />
    </mask></defs>
    <rect width="1254" height="1254" fill="currentColor" mask={`url(#${maskId})`} />
  </svg>;
}

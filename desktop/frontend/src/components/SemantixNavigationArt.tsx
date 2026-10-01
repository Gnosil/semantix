import { useId, type SVGProps } from "react";

const generatedArt = import.meta.glob<string>("../assets/navigation/*.png", {
 eager: true, query: "?url", import: "default",
});

// Original Semantix pictograms. A margin, a record and an open corner are the
// recurring forms. No borrowed cube, robot, plug, sparkle or people silhouettes.
export const semantixArtPaths = {
 general: "M4 7V3h16v18H4v-4 M8 3v18 M11 7h6m-6 5h4m-4 5h6",
 models: "M3 8V3h12v15H3 M7 6h12v15H7 M11 10h5m-5 4h3",
 bots: "M3 3h12v9H7l-4 4z M12 8h9v13H10v-5 M6 7h6m3 7h3m-3 3h3",
 providers: "M3 4h6v6H3z M15 14h6v6h-6z M9 7h9v7 M6 10v7h9",
 mcp: "M3 3h8v8H3z M13 13h8v8h-8z M11 7h6v6 M7 11v6h6 M5 7h4m6 10h4",
 remote: "M2 3h8v14H2z M14 7h8v14h-8z M5 7h2m10 4h2 M10 10h4m-2-3 3 3-3 3",
 skills: "M4 3h12l4 4v14H4z M16 3v5h4 M8 17l3-7h5l-3 7z M8 7h4",
 subagents: "M9 2h6v6H9z M2 16h6v6H2z M16 16h6v6h-6z M12 8v4H5v4m7-4h7v4 M4 19h2m12 0h2",
 plugins: "M4 4h11v6h6v11H4z M15 4h6v6h-6 M8 9h3m-3 5h9m-9 3h6",
 memory: "M3 8V3h12 M6 12V6h12 M9 9h12v12H9z M12 13h6m-6 4h4 M3 17v4h3",
 hooks: "M3 3h11v11H3z M7 7h3 M14 7h7v14H7v-7 M11 17h6m-3-3 3 3-3 3",
 diagnostics: "M4 3h16 M6 3v4m4-4v2m4-2v4m4-4v2 M3 10h18v11H3z M7 17h3v-3h4v-2h3",
 shortcuts: "M8 3H3v18h5 M16 3h5v18h-5 M8 8v8m8-8v8 M8 12h8 M11 8h2m-2 8h2",
 permissions: "M3 9V3h18v18H3v-6 M8 8h9v9H8z M1 12h11m-3-3 3 3-3 3",
 sandbox: "M3 8V3h5m8 0h5v5m0 8v5h-5m-8 0H3v-5 M8 8h8v8H8z M10 12h4",
 network: "M3 3h7v7H3z M14 3h7v7h-7z M8 15h8v6H8z M10 7h4 M6 10v8h2m8 0h2v-8",
 appearance: "M4 3h16v18H4z M4 8h16 M9 8v13 M12 12h5m-5 4h3 M7 5h1",
 storage: "M3 4h18v5H3z M3 12h18v4H3z M3 19h18v3H3z M6 6h2m-2 8h2m-2 6h2",
 updates: "M4 8V3h16v18H4v-7 M8 16h8 M12 13V6m-3 3 3-3 3 3",
 session: "M4 3h16v18H4z M8 3v18 M11 7h6m-6 5h3m1 3v5m-2-2h5",
 trash: "M3 3h14v14H3z M7 7h14v14H7 M10 11h7m-7 4h4 M3 11h4",
 automation: "M3 3h18v18H3z M7 7h10v4H7z M7 15h4v3H7z M15 15h2m-2 3h2",
 folder: "M3 4h6l3 3h9v14H3z M3 11h18 M7 15h5",
} as const;

export type SemantixArtName = keyof typeof semantixArtPaths;
export function SemantixNavigationArt({name, size=24, ...props}: SVGProps<SVGSVGElement> & {name:SemantixArtName;size?:number}) {
 const maskId = `semantix-art-${useId()}`;
 const assetName = name === "providers" ? "mcp" : name;
 const source = generatedArt[`../assets/navigation/${assetName}.png`];
 return <svg {...props} width={size} height={size} viewBox="0 0 24 24" fill="none" aria-hidden="true" data-semantix-art={name}>
  {source ? <>
   <defs><mask id={maskId} x="0" y="0" width="24" height="24" maskUnits="userSpaceOnUse" style={{maskType:"alpha"}}><image href={source} width="24" height="24" /></mask></defs>
   <rect width="24" height="24" fill="currentColor" mask={`url(#${maskId})`} />
  </> : <path d={semantixArtPaths[name]} stroke="currentColor" strokeWidth="1.1" strokeLinecap="butt" strokeLinejoin="miter"/>}
 </svg>;
}

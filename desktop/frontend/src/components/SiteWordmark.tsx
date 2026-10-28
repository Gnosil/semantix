import wordmark from "../assets/site-wordmark.svg";
export function SiteWordmark({ className = "" }: { className?: string }) {
  return <span className={"semantix-wordmark " + className}><img src={wordmark} alt="SEMANTIX" draggable={false}/></span>;
}

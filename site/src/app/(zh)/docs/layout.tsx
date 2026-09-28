import DocsSidebar from "@/components/DocsSidebar";
import DocsScrollReset from "@/components/DocsScrollReset";
import Nav from "@/components/Nav";
import { documents } from "@/lib/docs";

export default function DocsLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <>
      <Nav />
      <DocsScrollReset />
      <main className="min-h-[100dvh] bg-background pt-16">
        <div className="mx-auto grid min-h-[calc(100dvh-4rem)] w-full max-w-[1440px] lg:grid-cols-[280px_minmax(0,1fr)]">
          <DocsSidebar documents={documents} />
          <div className="min-w-0 lg:border-l lg:border-border">{children}</div>
        </div>
      </main>
    </>
  );
}

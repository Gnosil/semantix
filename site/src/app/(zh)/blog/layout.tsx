import Footer from "@/components/Footer";
import Nav from "@/components/Nav";

export default function BlogLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <>
      <Nav />
      <main className="min-h-[calc(100dvh-4rem)] bg-background pt-16">{children}</main>
      <Footer />
    </>
  );
}

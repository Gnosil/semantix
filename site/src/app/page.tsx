import Nav from "@/components/Nav";
import Hero from "@/components/Hero";
import Features from "@/components/Features";
import Components from "@/components/Components";
import Roadmap from "@/components/Roadmap";
import Community from "@/components/Community";
import Install from "@/components/Install";
import Footer from "@/components/Footer";
import BrandIntroOverlay from "@/components/BrandIntroOverlay";
import { siteIdentity } from "@/lib/site-identity";

const webpageJsonLd = {
  "@context": "https://schema.org",
  "@type": "WebPage",
  "@id": `${siteIdentity.productUrl}/#webpage`,
  url: `${siteIdentity.productUrl}/`,
  name: `${siteIdentity.productName} - a verifiable memory kernel for agents`,
  dateModified: siteIdentity.lastUpdated,
};

export default function Home() {
  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify(webpageJsonLd).replace(/</g, "\\u003c"),
        }}
      />
      <Nav />
      <main>
        <BrandIntroOverlay />
        <Hero />
        <Features />
        <Components />
        <Roadmap />
        <Community />
        <Install />
      </main>
      <Footer />
    </>
  );
}

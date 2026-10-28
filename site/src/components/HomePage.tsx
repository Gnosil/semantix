import { homePath, type LocaleProps } from "@/lib/i18n";
import Nav from "@/components/Nav";
import Hero from "@/components/Hero";
import Features from "@/components/Features";
import Components from "@/components/Components";
import Community from "@/components/Community";
import Install from "@/components/Install";
import BloomCurtain from "@/components/BloomCurtain";
import FinaleInfo from "@/components/FinaleInfo";
import BrandIntroOverlay from "@/components/BrandIntroOverlay";
import { siteIdentity } from "@/lib/site-identity";

export default function HomePage({ locale = "zh-CN" }: LocaleProps) {
  const url = `${siteIdentity.productUrl}${homePath(locale)}`;
  const webpageJsonLd = {
    "@context": "https://schema.org",
    "@type": "WebPage",
    "@id": `${url}#webpage`,
    url,
    inLanguage: locale,
    name: `${siteIdentity.productName} - a verifiable memory kernel for agents`,
    dateModified: siteIdentity.lastUpdated,
  };

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{
          __html: JSON.stringify(webpageJsonLd).replace(/</g, "\\u003c"),
        }}
      />
      <Nav locale={locale} />
      <main>
        <BrandIntroOverlay locale={locale} />
        <Hero locale={locale} />
        <Features locale={locale} />
        <Components locale={locale} />
        <Community locale={locale} />
        <Install locale={locale} />
        <BloomCurtain />
      </main>
      <FinaleInfo locale={locale} />
    </>
  );
}

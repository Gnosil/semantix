import type { Metadata } from "next";
import { homePath, type Locale } from "@/lib/i18n";
import { siteIdentity } from "@/lib/site-identity";

export const siteMetadata: Metadata = {
  metadataBase: new URL(siteIdentity.productUrl),
  title: "Semantix - a verifiable memory kernel for agents",
  description: "An open-source Go memory kernel with semantic slice extraction, BM25 retrieval, stable injection, and explicit experimental boundaries.",
  alternates: { canonical: "/" },
  icons: [{ rel: "icon", url: "/seo/favicon.svg", type: "image/svg+xml" }],
};

export function homeMetadata(locale: Locale): Metadata {
  return {
    ...siteMetadata,
    alternates: {
      canonical: homePath(locale),
      languages: { "zh-CN": "/", en: "/en/", "x-default": "/" },
    },
    openGraph: {
      title: "Semantix - a verifiable memory kernel for agents",
      description: siteMetadata.description!,
      url: homePath(locale),
      locale: locale === "en" ? "en_US" : "zh_CN",
      alternateLocale: locale === "en" ? "zh_CN" : "en_US",
    },
  };
}

export const siteIdentity = {
  productName: "Semantix",
  productUrl: "https://semantix.ensureok.ai",
  repositoryUrl: "https://github.com/Gnosil/semantix",
  licenseName: "MIT",
  lastUpdated: "2026-09-24",
  operator: {
    legalName: "确石人工智能科技（上海）有限公司",
    brandName: "确石智能",
    englishName: "Queshi Intelligence",
    url: "https://www.ensureok.ai/",
    logoUrl: "https://www.ensureok.ai/ensureok-logo.png",
    email: "junhaihuang@aiqueshi.com",
  },
} as const;

export const organizationJsonLd = {
  "@context": "https://schema.org",
  "@type": "Organization",
  "@id": `${siteIdentity.operator.url}#organization`,
  name: siteIdentity.operator.legalName,
  alternateName: [
    siteIdentity.operator.brandName,
    siteIdentity.operator.englishName,
  ],
  url: siteIdentity.operator.url,
  logo: siteIdentity.operator.logoUrl,
  email: siteIdentity.operator.email,
  sameAs: [siteIdentity.repositoryUrl, siteIdentity.operator.url],
  contactPoint: {
    "@type": "ContactPoint",
    email: siteIdentity.operator.email,
    url: `${siteIdentity.productUrl}/contact`,
    contactType: "project inquiries",
    availableLanguage: ["zh-CN", "en"],
  },
};

export const websiteJsonLd = {
  "@context": "https://schema.org",
  "@type": "WebSite",
  "@id": `${siteIdentity.productUrl}/#website`,
  url: `${siteIdentity.productUrl}/`,
  name: siteIdentity.productName,
  inLanguage: ["zh-CN", "en"],
  publisher: { "@id": `${siteIdentity.operator.url}#organization` },
};

export const softwareApplicationJsonLd = {
  "@context": "https://schema.org",
  "@type": "SoftwareApplication",
  "@id": `${siteIdentity.productUrl}/#software-application`,
  name: siteIdentity.productName,
  url: `${siteIdentity.productUrl}/`,
  description:
    "An open-source Go memory kernel with semantic slice extraction, BM25 retrieval, stable injection, and experimental scheduling and adaptation interfaces.",
  applicationCategory: "DeveloperApplication",
  operatingSystem: "Linux, macOS, Windows",
  license: "https://github.com/Gnosil/semantix/blob/main/LICENSE",
  codeRepository: siteIdentity.repositoryUrl,
  downloadUrl: siteIdentity.repositoryUrl,
  dateModified: siteIdentity.lastUpdated,
  offers: {
    "@type": "Offer",
    price: "0",
    priceCurrency: "USD",
    description: "Open source (MIT)",
  },
  publisher: { "@id": `${siteIdentity.operator.url}#organization` },
};

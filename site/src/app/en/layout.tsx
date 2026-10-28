import SiteDocument from "@/components/SiteDocument";
import { siteMetadata } from "@/lib/home-metadata";

export const metadata = siteMetadata;

export default function Layout({ children }: { children: React.ReactNode }) {
  return <SiteDocument locale="en">{children}</SiteDocument>;
}

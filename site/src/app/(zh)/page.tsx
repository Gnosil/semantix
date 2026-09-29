import HomePage from "@/components/HomePage";
import { homeMetadata } from "@/lib/home-metadata";

export const metadata = homeMetadata("zh-CN");

export default function Home() {
  return <HomePage locale="zh-CN" />;
}

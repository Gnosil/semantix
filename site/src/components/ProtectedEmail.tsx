import { siteIdentity } from "@/lib/site-identity";

type ProtectedEmailProps = {
  className?: string;
};

export default function ProtectedEmail({ className }: ProtectedEmailProps) {
  const [localPart, domain] = siteIdentity.operator.email.split("@");

  return (
    <span className={className} aria-label={siteIdentity.operator.email}>
      <span>{localPart}</span>
      <span aria-hidden="true">@</span>
      <span>{domain}</span>
    </span>
  );
}

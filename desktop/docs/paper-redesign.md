# Semantix Paper desktop

The desktop uses a white canvas, the website's deep green (#0f735c), a solid SEMANTIX wordmark, compact system sans text, and original thin navigation artwork. Interface and Settings share the font stack; conversation names use regular 13px text, including the selected row.

## Workspace

- A large central wordmark and one rotating suggestion welcome a new chat. The suggestion scrambles on hover or after three idle seconds; reduced motion displays text without scrambling.
- A compact rectangular composer sits at the bottom. Session options disclose Agent / Plan / Goal, work style, and Review actions / Delegate / Skip prompts. These labels retain the existing approval and sandbox semantics.
- Skills, Bots, and MCP & Tools open independent workspaces. A skill can be inserted into the existing draft; insertion never sends a message.
- Pinned, Projects, and Recents share the sidebar hierarchy. The footer shows gateway readiness and the selected workspace name. Automatically generated conversation titles follow the UI locale; user-defined titles remain verbatim.
- Settings occupies the full window, with compact navigation and rewritten descriptions in English, Simplified Chinese, and Traditional Chinese.

## Usage and files

New chats start with the right dock closed. Accepted work reveals Usage once. Finishing work leaves the numbers visible, while manually closing the dock keeps it closed for that chat. Existing chats retain their selected dock view.

Usage shows total, input/output tokens, requests, per-model shares, reported cache hit rates, memory matches, and context. Unknown values remain unavailable. Financial savings are not shown. Totals use a locally bundled OFL Bodoni Moda approximation of the requested Didone numeral style, with lowercase k; increasing totals roll upward. Fine magnetic ticks follow the pointer without per-pointer React rendering. Reduced motion disables the effects.

Files adapts Magic UI's tree to the existing lazy loading, virtualizer, selection, references, keyboard navigation, and context menus. Selected mode and usage-scope controls use a single green border beam instead of filled capsules. Magic UI's MIT notice and original reference sources are included beside the adapted components; fonts include their OFL notices.

## Compatibility

The usage dock restores the ContextPanel interface and its persisted latest-turn/read telemetry. Remote work surfaces retain their existing host-bound file/port/server actions and tests. Main-branch request/elapsed counters and the context-only memory admission policy remain intact. New statistics fields are additive; memory/context activity does not increment provider request totals. No model provider needs to be connected to view the interface.

Native macOS and Chrome previews were inspected using an isolated test home. Screenshots show native UI, not a live provider run. Windows/Linux packaging and live provider usage remain for follow-up verification.

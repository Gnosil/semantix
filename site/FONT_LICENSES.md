# Self-hosted fonts

The Semantix site bundles its web fonts from the packages below. The versions
are pinned in `package.json` and `package-lock.json`, so `next build` does not
fetch fonts from Google or another external font host.

| Font | Package | Version | Upstream | License |
| --- | --- | --- | --- | --- |
| Inter | `@fontsource-variable/inter` | 5.3.0 | <https://github.com/rsms/inter> | SIL Open Font License 1.1 |
| JetBrains Mono | `@fontsource-variable/jetbrains-mono` | 5.3.0 | <https://github.com/JetBrains/JetBrainsMono> | SIL Open Font License 1.1 |
| Noto Sans SC | `@fontsource-variable/noto-sans-sc` | 5.3.0 | <https://github.com/notofonts/noto-cjk> | SIL Open Font License 1.1 |

The packaged font files and CSS are distributed by the
[Fontsource font-files repository](https://github.com/fontsource/font-files).
Each installed package includes the full `LICENSE` text. The license is also
available at <https://openfontlicense.org/open-font-license-official-text/>.

Noto Sans SC remains split into Unicode-range WOFF2 files by Fontsource. This
keeps the Chinese display face available without forcing every browser to
download the complete CJK character set.


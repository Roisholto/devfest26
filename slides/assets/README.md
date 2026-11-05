# Visual source

The theme adapts the user-supplied `[External] DevFest 2026 Intro Slides- goo.gle_devfest26-intro-slides.pdf` (16:9, especially pages 7, 11 and 12). The source deck explicitly permits copying/localizing/expanding it for local communities.

`gdg-mark.svg` contains the original GDG mark extracted from that PDF (image object 92 with its original alpha), cropped by an SVG viewBox rather than redrawn. `architecture.svg` is an original vector diagram for this workshop. No generic event statistics or photographs are presented as workshop evidence.

Typography uses bundled **Google Sans** (regular/medium/semibold/bold) and **Roboto Mono** (light/code), matching the font families and weights identified in the source PDF. HTML loads them locally with `@font-face`; PDF export embeds them. The font assets are copied before PDF rendering and work offline, without relying on installed fonts. No subset PDF font is repackaged as a full typeface.

The unmodified static fonts are served by Google's official Fonts API, with exact download URLs recorded in `fonts/SOURCES.md`. The families are published in Google's official repository: [Google Sans](https://github.com/google/fonts/tree/main/ofl/googlesans) and [Roboto Mono](https://github.com/google/fonts/tree/main/ofl/robotomono). Their SIL Open Font License notices are included alongside the fonts in `fonts/`. The architecture SVG embeds a Google Fonts text subset of Google Sans so image rendering also uses the correct typeface.

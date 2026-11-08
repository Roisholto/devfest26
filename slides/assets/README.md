# Visual source

The theme adapts the user-supplied `[External] DevFest 2026 Intro Slides- goo.gle_devfest26-intro-slides.pdf` (16:9, especially pages 7, 11 and 12). The source deck explicitly permits copying/localizing/expanding it for local communities.

`gdg-mark.svg` contains the original GDG mark extracted from that PDF (image object 92 with its original alpha), cropped by an SVG viewBox rather than redrawn. `architecture.svg` and `durable-vs-memory.svg` are original vector diagrams for this workshop. The recovery diagram compares loss of in-memory progress against history-driven replay after a worker stops. No generic event statistics or photographs are presented as workshop evidence.

Typography uses bundled **Google Sans** (regular/medium/semibold/bold) and **Roboto Mono** (light/code), matching the font families and weights identified in the source PDF. HTML loads them locally with `@font-face`; PDF export embeds them. The font assets are copied before PDF rendering and work offline, without relying on installed fonts. No subset PDF font is repackaged as a full typeface.

The unmodified static fonts are served by Google's official Fonts API, with exact download URLs recorded in `fonts/SOURCES.md`. The families are published in Google's official repository: [Google Sans](https://github.com/google/fonts/tree/main/ofl/googlesans) and [Roboto Mono](https://github.com/google/fonts/tree/main/ofl/robotomono). Their SIL Open Font License notices are included alongside the fonts in `fonts/`. Both diagram SVGs embed official Google Fonts text subsets of Google Sans so image rendering also uses the correct typeface offline. The recovery comparison uses a regular-weight ASCII subset from the Google Fonts CSS API; the bundled Google Sans license applies.

`github-repo-qr.svg` is a locally generated QR code for [Roisholto/netlify-load-external-files](https://github.com/Roisholto/netlify-load-external-files). It encodes that direct URL, uses a four-module white quiet zone and medium error correction, and requires no network access to render. The same asset appears in the closing slide and README; both also include clickable text links.

`workshop-repo-qr.svg` encodes [Roisholto/devfest26](https://github.com/Roisholto/devfest26), using the same quiet zone and error correction. The workshop access slide pairs it with a clickable URL, clone commands and a direct link to the participant setup guide.

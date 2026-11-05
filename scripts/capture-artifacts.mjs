// Optional presenter utility: capture actual deck/evidence pages, not invented results.
import puppeteer from 'puppeteer-core';
import { existsSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { pathToFileURL } from 'node:url';
import path from 'node:path';

const executablePath = process.env.CHROME_PATH || path.join(homedir(), '.cache/ms-playwright/chromium-1217/chrome-linux64/chrome');
const browser = await puppeteer.launch({ executablePath, headless: true, args: ['--no-sandbox'] });
try {
  const page = await browser.newPage();
  await page.setViewport({ width: 1280, height: 720, deviceScaleFactor: 1 });
  // Print mode renders every slide, including code on otherwise hidden pages.
  await page.emulateMediaType('print');
  await page.setRequestInterception(true);
  page.on('request', request => /^https?:/.test(request.url()) ? request.abort() : request.continue());
  await page.goto(pathToFileURL(path.resolve('dist/workshop.html')).href, { waitUntil: 'networkidle0' });
  await page.evaluate(() => document.fonts.ready);
  // Check the fonts Chromium actually used, rather than trusting the CSS name
  // or document.fonts.check(), which can succeed when a fallback is selected.
  const cdp = await page.createCDPSession();
  await cdp.send('DOM.enable');
  await cdp.send('CSS.enable');
  const { root } = await cdp.send('DOM.getDocument');
  const fontChecks = [];
  for (const [selector, expected] of [['h1', 'GoogleSans-SemiBold'], ['code', 'RobotoMono-Light']]) {
    const { nodeId } = await cdp.send('DOM.querySelector', { nodeId: root.nodeId, selector });
    if (!nodeId) throw new Error('Missing font verification element: ' + selector);
    const { fonts } = await cdp.send('CSS.getPlatformFontsForNode', { nodeId });
    const passed = fonts.some(font => font.postScriptName === expected && font.isCustomFont && font.glyphCount > 0);
    fontChecks.push({ selector, expected, fonts, passed });
    if (!passed) throw new Error('Bundled font was not rendered: ' + expected);
  }
  writeFileSync('dist/font-check.json', JSON.stringify({ offline: true, checks: fontChecks }, null, 2));
  const layout = await page.evaluate(() => [...document.querySelectorAll('section')].map((s, i) => ({ slide: i + 1, title: s.querySelector('h1')?.textContent, overflow: s.scrollHeight > s.clientHeight + 2 || s.scrollWidth > s.clientWidth + 2 })));
  writeFileSync('dist/layout-check.json', JSON.stringify(layout, null, 2));
  if (layout.some(s => s.overflow)) throw new Error('Slide overflow detected: ' + JSON.stringify(layout.filter(s => s.overflow)));
  await page.screenshot({ path: 'dist/title-slide.png' });
  const dir = path.resolve('dist/fallback');
  if (existsSync(path.join(dir, 'index.html'))) {
    await page.goto(pathToFileURL(path.join(dir, 'index.html')).href, { waitUntil: 'networkidle0' });
    for (const name of ['opening', 'restart', 'lost-response']) {
      await page.evaluate(name => document.getElementById(name).scrollIntoView(), name);
      await page.screenshot({ path: path.join(dir, name + '.png') });
    }
  }
  console.log(`PASS: ${layout.length} slides without overflow; bundled Google Sans/Roboto Mono render offline; deck/evidence captures saved to dist.`);
} finally { await browser.close(); }

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
  await page.goto(pathToFileURL(path.resolve('dist/workshop.html')).href, { waitUntil: 'networkidle0' });
  const layout = await page.evaluate(() => [...document.querySelectorAll('section')].map((s, i) => ({ slide: i + 1, title: s.querySelector('h1')?.textContent, overflow: s.scrollHeight > s.clientHeight + 2 || s.scrollWidth > s.clientWidth + 2 })));
  writeFileSync('dist/layout-check.json', JSON.stringify(layout, null, 2));
  if (layout.some(s => s.overflow)) throw new Error('Slide overflow detected: ' + JSON.stringify(layout.filter(s => s.overflow)));
  await page.screenshot({ path: 'dist/title-slide.png' });
  for (const instance of ['presenter', 'participant']) {
    const dir = path.resolve('dist/fallback', instance);
    if (!existsSync(path.join(dir, 'index.html'))) continue;
    await page.goto(pathToFileURL(path.join(dir, 'index.html')).href, { waitUntil: 'networkidle0' });
    for (const name of ['opening', 'restart', 'lost-response']) {
      await page.evaluate(name => document.getElementById(name).scrollIntoView(), name);
      await page.screenshot({ path: path.join(dir, name + '.png') });
    }
  }
  console.log(`PASS: ${layout.length} slides without overflow; deck/evidence captures saved to dist.`);
} finally { await browser.close(); }

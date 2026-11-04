import { execFileSync } from 'node:child_process';
import { mkdirSync, cpSync, existsSync } from 'node:fs';
import { homedir } from 'node:os';
import path from 'node:path';

mkdirSync('dist', { recursive: true });
// A real native Chrome binary is needed on WSL; avoid a broken snap shim.
const cachedChrome = path.join(homedir(), '.cache/ms-playwright/chromium-1217/chrome-linux64/chrome');
if (!process.env.CHROME_PATH && existsSync(cachedChrome)) process.env.CHROME_PATH = cachedChrome;
const marp = path.resolve('node_modules/.bin/marp');
const common = ['slides/workshop.md', '--theme-set', 'slides/theme.css', '--allow-local-files'];
execFileSync(marp, [...common, '-o', 'dist/workshop.html'], { stdio: 'inherit' });
execFileSync(marp, [...common, '--pdf', '-o', 'dist/workshop.pdf'], { stdio: 'inherit' });
// HTML remains usable offline from dist rather than relying on source paths.
cpSync('slides/assets', 'dist/assets', { recursive: true });

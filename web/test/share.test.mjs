import assert from 'node:assert/strict';
import fs from 'node:fs';
import test from 'node:test';
import {initWasm, Terminal} from 'rioterm';
import {MISSING, failureSentence, feed, fitFontSize, fontStack, relativeTime, rioTheme, shareId, sharedLine} from '../static/share-lib.js';

const palette = ['#000000', '#cd3131', '#0dbc79', '#e5e510', '#2472c8', '#bc3fbc', '#11a8cd', '#e5e5e5',
  '#666666', '#f14c4c', '#23d18b', '#f5f543', '#3b8eea', '#d670d6', '#29b8db', '#ffffff'];

const record = {
  id: 'abcdefghijklmnopqrstuvwxyz',
  created_at: '2026-11-18T10:00:00Z',
  expires_at: '2026-12-18T10:00:00Z',
  device: 'nelsonpc',
  cols: 24,
  rows: 3,
  // A prompt in color, a command's output with a Nerd Font icon and a wide character.
  vt: '\x1b[H\x1b[2J\x1b[1;32mnelson\x1b[0m:\x1b[34m~\x1b[0m$ ls\r\n\uf07b docs  \u4e2d\r\n\x1b[7mstatus\x1b[0m',
  title: 'at:1 shell',
  theme: {foreground: '#cccccc', background: '#1e1e1e', cursor: '#ffffff', palette},
  font: {family: 'CaskaydiaMono NF'},
};

test('a share page path names its id', () => {
  assert.equal(shareId('/s/abcdefghijklmnopqrstuvwxyz'), 'abcdefghijklmnopqrstuvwxyz');
  assert.equal(shareId('/s/abcdefghijklmnopqrstuvwxyz/'), 'abcdefghijklmnopqrstuvwxyz');
  assert.equal(shareId('/s/ABCDEFGHIJKLMNOPQRSTUVWXYZ'), null);
  assert.equal(shareId('/s/short'), null);
  assert.equal(shareId('/'), null);
});

test("the snapshot's palette becomes rioterm's theme in ANSI order", () => {
  const theme = rioTheme(record.theme);
  assert.equal(theme.foreground, '#cccccc');
  assert.equal(theme.background, '#1e1e1e');
  assert.equal(theme.cursor, '#ffffff');
  assert.equal(theme.black, palette[0]);
  assert.equal(theme.white, palette[7]);
  assert.equal(theme.brightBlack, palette[8]);
  assert.equal(theme.brightWhite, palette[15]);
});

test('the font stack puts the served font behind the snapshot one', () => {
  assert.equal(fontStack('CaskaydiaMono NF'), '"CaskaydiaMono NF", monospace');
  assert.equal(fontStack('Fira Code'), '"Fira Code", "CaskaydiaMono NF", monospace');
});

test('the font size fills the width', () => {
  assert.equal(fitFontSize(1200, 100, 0.6), 20);
  assert.equal(fitFontSize(300, 1000, 0.6), 4);
  assert.equal(fitFontSize(10000, 10, 0.6), 64);
  assert.equal(fitFontSize(0, 80, 0.6), 14);
});

test('the shared line says how long ago and when it expires', () => {
  const now = new Date('2026-11-18T10:05:00Z');
  assert.equal(relativeTime(new Date('2026-11-18T10:04:50Z'), now, 'en-US'), 'just now');
  assert.equal(relativeTime(new Date('2026-11-17T09:00:00Z'), now, 'en-US'), 'yesterday');
  assert.equal(sharedLine(record, now, 'en-US'), 'shared 5 minutes ago, expires Dec 18, 2026');
});

test('a failed load says why', async () => {
  const missing = new Response(JSON.stringify({error: MISSING}), {status: 404});
  assert.equal(await failureSentence(missing), MISSING);
  assert.equal(await failureSentence(new Response('gateway', {status: 404})), MISSING);
  assert.match(await failureSentence(new Response('', {status: 502})), /HTTP 502/);
});

test('rioterm draws the snapshot the page feeds it', async () => {
  // The page loads the wasm beside rioterm's glue by URL; Node is handed its bytes.
  const wasm = new URL('../wasm/librio_wasm_bg.wasm', import.meta.resolve('rioterm'));
  await initWasm(fs.readFileSync(wasm));
  const term = new Terminal({cols: record.cols, rows: record.rows, scrollback: 0, detectUrls: false});
  term.write(feed(record));
  assert.equal(term.textRow(0).trimEnd(), 'nelson:~$ ls');
  assert.equal(term.textRow(1).trimEnd(), '\uf07b docs  \u4e2d');
  assert.equal(term.textRow(2).trimEnd(), 'status');
  const snapshot = term.snapshot();
  assert.equal(snapshot.cols, record.cols);
  assert.equal(snapshot.rows, record.rows);
  assert.equal(snapshot.cursorVisible, false);
  term.dispose();
});

// Copies the engine the share page loads, rioterm's ES module and its WebAssembly, from
// node_modules into static/vendor/rioterm/, keeping the package's own layout: its module
// imports ../wasm/librio_wasm.js, and that loads librio_wasm_bg.wasm beside itself. The
// image's build stage runs this after `npm ci`; the Go binary embeds static/.
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const from = path.join(here, 'node_modules', 'rioterm');
const to = path.join(here, 'static', 'vendor', 'rioterm');
const files = ['package.json', 'dist/index.js', 'wasm/librio_wasm.js', 'wasm/librio_wasm_bg.wasm'];

fs.rmSync(to, {recursive: true, force: true});
for (const file of files) {
  const source = path.join(from, file);
  if (!fs.existsSync(source)) throw new Error(`rioterm has no ${file}: run npm ci first`);
  fs.mkdirSync(path.dirname(path.join(to, file)), {recursive: true});
  fs.copyFileSync(source, path.join(to, file));
}
const {version} = JSON.parse(fs.readFileSync(path.join(from, 'package.json'), 'utf8'));
console.log(`copied rioterm ${version} to static/vendor/rioterm`);

// The page's own scripts must parse as modules: a syntax error would leave the page on
// "Loading" with nothing said.
import {spawnSync} from 'node:child_process';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const scripts = ['static/share.js', 'static/share-lib.js', 'build.mjs', 'lint.mjs', 'test/share.test.mjs'];
let failed = false;
for (const script of scripts) {
  const result = spawnSync(process.execPath, ['--check', path.join(here, script)], {encoding: 'utf8'});
  if (result.status !== 0) {
    failed = true;
    process.stderr.write(`${script}:\n${result.stderr}`);
  }
}
if (failed) process.exit(1);
console.log(`${scripts.length} scripts parse`);
